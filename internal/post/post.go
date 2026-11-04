// Package post sends a prepared review to GitHub: the anchor guard, the review
// itself, its replies, and the artifact cleanup that follows a successful post.
package post

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kyleking/aragonite/forge/github"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/pool"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/resolve"
)

// rootOf is the checkout or state directory a prepared review is staged under.
func rootOf(path string) string { return filepath.Dir(filepath.Dir(path)) }

func reviewHost(r *artifact.Review) string {
	if r.Host == "" {
		return "github.com"
	}

	return r.Host
}

// ErrHeadMoved reports a pull request whose head advanced since the review
// was prepared.
var ErrHeadMoved = errors.New("the pull request has new commits")

// Poster sends one request, asks the GraphQL endpoint one question, and
// resolves threads. The gh CLI is the only implementation that ships; a test
// supplies its own.
type Poster interface {
	Post(ctx context.Context, endpoint string, body []byte) ([]byte, error)
	Delete(ctx context.Context, endpoint string) error
	GraphQL(ctx context.Context, request []byte) ([]byte, error)
	Resolve(ctx context.Context, threadID string) error
}

type ghPoster struct{}

// GH posts by shelling out to `gh api`.
//
//nolint:ireturn // Poster is the seam a test replaces; concrete would remove it
func GH() Poster { return ghPoster{} }

func (ghPoster) Post(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	//nolint:gosec // the endpoint is built from the artifact's own owner, repo, and number
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "POST", endpoint, "--input", "-")
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api POST %s: %w", endpoint, err)
	}

	return out, nil
}

// Delete is the pending-review cleanup: abandoning a review that replies were
// being added to keeps the pull request from carrying a half-staged review
// nobody can see until something else submits it.
func (ghPoster) Delete(ctx context.Context, endpoint string) error {
	//nolint:gosec // the endpoint is built from the artifact's own owner, repo, and number
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "DELETE", endpoint)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh api DELETE %s: %w", endpoint, err)
	}

	return nil
}

func (ghPoster) GraphQL(ctx context.Context, request []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", "api", "graphql", "--input", "-")
	cmd.Stdin = strings.NewReader(string(request))
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
	}

	return out, nil
}

func (ghPoster) Resolve(ctx context.Context, threadID string) error {
	//nolint:wrapcheck // resolve.Thread names the thread in its own message
	return resolve.Thread(ctx, ghrun.GH(), "", threadID)
}

// Guard compares every comment against the pull request's current diff before
// anything is sent. A comment whose line moved but whose text did not is
// relocated and reported to out; one whose text no longer matches anywhere
// would land on whatever now sits there, which is worse than not posting it.
//
// The directory gh runs in is dir, and remoteRepo names the repository when that
// directory does not, which is how a review prepared with no checkout is
// guarded from anywhere.
func Guard(ctx context.Context, dir, remoteRepo string, r *artifact.Review, out io.Writer) error {
	pr, err := github.GetPR(ctx, dir, remoteRepo, r.Number)
	if err != nil {
		return fmt.Errorf("checking the pull request head: %w", err)
	}
	if pr.HeadSHA != r.HeadSHA {
		return fmt.Errorf("%w: prepared against %s, now at %s; run second-look get %d",
			ErrHeadMoved, r.HeadSHA, pr.HeadSHA, r.Number)
	}

	patch, err := github.PRDiff(ctx, dir, remoteRepo, r.Number)
	if err != nil {
		// Over the API's caps the diff is computed locally the same way get
		// does, so a review big enough to refuse is still postable.
		patch, err = pool.Diff(ctx, out, dir, reviewHost(r), r.Owner, r.Repo, r.BaseRef, r.HeadSHA)
		if err != nil {
			return fmt.Errorf("reading the current diff: %w", err)
		}
	}

	moved, err := artifact.Verify(r.Comments, diff.Parse(patch))
	if err != nil {
		return fmt.Errorf("nothing was posted:\n%w", err)
	}

	for _, m := range moved {
		if _, err := fmt.Fprintf(out, "relocated %s\n", m); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	}

	return nil
}

// Run posts an already-guarded review — its comments as threads on a pending
// review, its replies against that review by name, and one submit that makes
// all of it visible at once — then records what posted and removes the
// artifact file at path.
func Run(ctx context.Context, p Poster, path string, r *artifact.Review, out io.Writer) error {
	threads, replies, err := r.Threads()
	if err != nil {
		return fmt.Errorf("building the payload: %w", err)
	}

	submitted, err := postReview(ctx, p, r, threads, replies)
	if err != nil {
		return err
	}

	if err := write(out, fmt.Sprintf("posted review %d on %s/%s#%d\n",
		submitted.DatabaseID, r.Owner, r.Repo, r.Number)); err != nil {
		return err
	}

	// A resolve that fails leaves the review posted either way, so the record
	// and the cleanup still run; the failure is reported once both are done.
	unresolved := resolveMarked(ctx, p, replies, submitted.DatabaseID, out)

	// The record goes down before the staged file goes away, so what GitHub
	// holds outlives the artifact that described it. A record that cannot be
	// written keeps the staged file rather than leaving neither.
	if err := writeRecord(rootOf(path), r, submitted.DatabaseID, submitted.ID, "review"); err != nil {
		return fmt.Errorf("the review posted as %d; keeping the staged file as its record: %w",
			submitted.DatabaseID, err)
	}

	// GitHub is the source of truth from here, and a prepared review left on
	// disk would post a second copy of itself if anyone ran post again. The
	// artifact goes first, then its read marks, its todo files, and whatever
	// was cached against the head.
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("the review posted; removing %s: %w", path, err)
	}

	if err := prepared.DiscardAt(rootOf(path), r.Number); err != nil {
		return fmt.Errorf("the review posted; clearing its files: %w", err)
	}

	if err := prunePublished(path, r, out); err != nil {
		return err
	}

	if unresolved != nil {
		return unresolved
	}

	return write(out, fmt.Sprintf("removed the staged review for %s/%s#%d\n", r.Owner, r.Repo, r.Number))
}

// resolveMarked closes the threads the posted replies marked for resolving
// and answers the first failure, leaving the rest unasked.
func resolveMarked(
	ctx context.Context, p Poster, replies []artifact.ReplyPayload, reviewID int64, out io.Writer,
) error {
	var unresolved error

	for _, reply := range replies {
		if !reply.Resolve || reply.ThreadID == "" || unresolved != nil {
			continue
		}

		if err := resolveAfter(ctx, p, reply.ThreadID, out); err != nil {
			unresolved = fmt.Errorf("the review posted as %d; %w", reviewID, err)
		}
	}

	return unresolved
}

// prunePublished drops a local review staged for the head ref that just
// published, so it is not carried as a second copy of the same work.
func prunePublished(path string, r *artifact.Review, out io.Writer) error {
	local, err := artifact.Load(artifact.LocalPath(rootOf(path)))
	if err != nil || local.HeadRef != r.HeadRef || !prepared.DiscardLocal(rootOf(path)) {
		return nil //nolint:nilerr // no local review is the common case, not a failure
	}

	return write(out, "removed the local review, now published\n")
}

// postReview carries a staged review through the pending-review dance: open
// one holding the comments, add each reply to it by id, then submit. Any
// failure between open and submit abandons the pending review, because one
// left behind is a reply's invisible landing place — the failure that lost a
// reply while reporting success.
func postReview(
	ctx context.Context, p Poster, r *artifact.Review,
	threads []artifact.DraftThread, replies []artifact.ReplyPayload,
) (*pendingReview, error) {
	prID, err := prNodeID(ctx, p, r)
	if err != nil {
		return nil, err
	}

	pending, err := openReview(ctx, p, prID, r, threads)
	if err != nil {
		return nil, err
	}

	var lookup map[int64]string

	for _, reply := range replies {
		tid := reply.ThreadID

		if tid == "" {
			var err error
			if lookup == nil {
				lookup, err = threadIDs(ctx, p, r)
				if err != nil {
					return nil, abandon(ctx, p, r, pending, err)
				}
			}

			tid = lookup[reply.InReplyTo]
		}

		if tid == "" {
			return nil, abandon(ctx, p, r, pending,
				fmt.Errorf("reply %s answers comment %d, which is %w",
					reply.ID, reply.InReplyTo, errNotAThread))
		}

		if err := addReply(ctx, p, pending.ID, tid, reply.Body); err != nil {
			return nil, abandon(ctx, p, r, pending, fmt.Errorf("reply %s: %w", reply.ID, err))
		}
	}

	event := r.Event
	if event == "" {
		event = artifact.EventComment
	}

	submitted, err := submitReview(ctx, p, pending.ID, event, r.Body)
	if err != nil {
		return nil, abandon(ctx, p, r, pending, err)
	}

	return submitted, nil
}

// abandon deletes the pending review a failed post left open and reports what
// happened to it. Nothing it held was public, so retrying is clean — which the
// message says, because a review deleted unseen is still a review that had to
// be deleted.
func abandon(ctx context.Context, p Poster, r *artifact.Review, pending *pendingReview, cause error) error {
	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews/%d",
		r.Owner, r.Repo, r.Number, pending.DatabaseID)

	if err := p.Delete(ctx, endpoint); err != nil {
		return fmt.Errorf("%w; the pending review it left (id %d) could not be deleted "+
			"either — close it on GitHub before posting again: %v",
			cause, pending.DatabaseID, err)
	}

	return fmt.Errorf("%w; the pending review it left was deleted, "+
		"nothing reached GitHub, and the staged review is still on disk", cause)
}

// One posts a single comment on its own, outside any review, and takes it out
// of the prepared review afterwards.
//
// It is for the finding worth saying now rather than at the end: a build that
// is broken for everyone, a secret in a diff. The rest of the review stays
// staged, and GitHub owns the comment from the moment it lands, so it is
// removed here for the same reason a posted review's artifact is deleted.
func One(ctx context.Context, p Poster, path string, r *artifact.Review, id string, out io.Writer) error {
	payload, c, err := r.OnePayload(id)
	if err != nil {
		return fmt.Errorf("posting %s on its own: %w", id, err)
	}

	postedID, nodeID, kind, err := postOne(ctx, p, r, payload, c, id, out)
	if err != nil {
		return err
	}

	if c.Resolve && c.ThreadID != "" {
		if err := resolveAfter(ctx, p, c.ThreadID, out); err != nil {
			return err
		}
	}

	if err := writeRecord(rootOf(path), r, postedID, nodeID, kind); err != nil {
		return fmt.Errorf("the comment posted; writing its record: %w", err)
	}

	r.Remove(id)

	if err := artifact.Save(path, r); err != nil {
		return fmt.Errorf("the comment posted; rewriting %s: %w", path, err)
	}

	return write(out, fmt.Sprintf("removed %s from the staged review for %s/%s#%d\n", id, r.Owner, r.Repo, r.Number))
}

// postOne sends the one comment or reply One picked, the way each goes out: a
// standalone comment through the comments endpoint, a reply inside a pending
// review of its own, because naming the review is what keeps a reply visible.
// It answers the id GitHub gave what posted and whether that was a comment or
// a review.
func postOne(
	ctx context.Context, p Poster, r *artifact.Review,
	payload any, c *artifact.Comment, id string, out io.Writer,
) (int64, string, string, error) {
	if c.InReplyTo != 0 {
		return postReplyOne(ctx, p, r, id, out)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, "", "", fmt.Errorf("encoding the comment: %w", err)
	}

	body, err = withCommit(body, r.HeadSHA)
	if err != nil {
		return 0, "", "", err
	}

	raw, err := p.Post(ctx, commentEndpoint(r), body)
	if err != nil {
		return 0, "", "", err //nolint:wrapcheck // the caller reports the raw gh failure verbatim
	}

	var sent struct {
		ID     int64  `json:"id"`
		NodeID string `json:"node_id"`
	}

	var postedID int64

	var nodeID string

	if err := json.Unmarshal(raw, &sent); err == nil {
		postedID, nodeID = sent.ID, sent.NodeID
	}

	if err := write(out, fmt.Sprintf("posted comment %d\n", postedID)); err != nil {
		return 0, "", "", err
	}

	return postedID, nodeID, "comment", nil
}

// postReplyOne runs one staged reply through the same pending-review dance a
// whole review takes, since the reply names the review it belongs to by id.
func postReplyOne(
	ctx context.Context, p Poster, r *artifact.Review, id string, out io.Writer,
) (int64, string, string, error) {
	one := r.Only(id)

	threads, replies, err := one.Threads()
	if err != nil {
		return 0, "", "", fmt.Errorf("posting %s on its own: %w", id, err)
	}

	submitted, err := postReview(ctx, p, one, threads, replies)
	if err != nil {
		return 0, "", "", err
	}

	if err := write(out, fmt.Sprintf("posted %s in review %d\n", id, submitted.DatabaseID)); err != nil {
		return 0, "", "", err
	}

	return submitted.DatabaseID, submitted.ID, "review", nil
}

// withCommit adds the head the comment was anchored against. A standalone
// comment names its own commit, where a review names it once for all of them.
func withCommit(body []byte, sha string) ([]byte, error) {
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("encoding the comment: %w", err)
	}

	fields["commit_id"] = sha

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encoding the comment: %w", err)
	}

	return out, nil
}

// DryRun prints what Run would send without sending it: the four GraphQL
// calls the review posts through, with the replies spelled out one each.
func DryRun(out io.Writer, r *artifact.Review) error {
	threads, replies, err := r.Threads()
	if err != nil {
		return fmt.Errorf("building the payload: %w", err)
	}

	event := r.Event
	if event == "" {
		event = artifact.EventComment
	}

	add, err := request(addReviewMutation, map[string]any{keyInput: map[string]any{
		"pullRequestId": "<the pull request's node id>", "commitOID": r.HeadSHA,
		"threads": threads,
	}})
	if err != nil {
		return fmt.Errorf("encoding the review: %w", err)
	}

	if err := write(out, fmt.Sprintf("graphql %s\n%s\n\n", prNodeQuery, add)); err != nil {
		return err
	}

	for _, reply := range replies {
		body, err := request(addReplyMutation, map[string]any{keyInput: map[string]any{
			keyPullRequestReviewID:  "<the pending review's id>",
			keyPullRequestReviewTID: reply.ThreadID,
			keyBody:                 reply.Body,
		}})
		if err != nil {
			return fmt.Errorf("encoding a reply: %w", err)
		}

		line := fmt.Sprintf("graphql %s\n", body)
		if reply.Resolve {
			line += "RESOLVE thread " + reply.ThreadID + "\n"
		}

		if err := write(out, line+"\n"); err != nil {
			return err
		}
	}

	submit, err := request(submitReviewMutation, map[string]any{keyInput: map[string]any{
		keyPullRequestReviewID: "<the pending review's id>", "event": event, keyBody: r.Body,
	}})
	if err != nil {
		return fmt.Errorf("encoding the review: %w", err)
	}

	return write(out, fmt.Sprintf("graphql %s\n", submit))
}

// resolveAfter closes the thread a posted reply answered. The reply is already
// on GitHub, so a failure here says that rather than suggesting a retry would
// be clean.
func resolveAfter(ctx context.Context, p Poster, threadID string, out io.Writer) error {
	if err := p.Resolve(ctx, threadID); err != nil {
		return fmt.Errorf("the reply posted but its thread did not resolve: %w", err)
	}

	return write(out, "resolved thread "+threadID+"\n")
}

// Summary is the one line that says what Run sent. What was not sent is said
// too: a skipped comment posts nothing, and a summary that only counts what
// went out leaves a missing reply looking like it landed.
func Summary(r *artifact.Review) string {
	var comments, replies, resolved int

	var skipped []string

	for i := range r.Comments {
		c := &r.Comments[i]

		switch {
		case c.Status == artifact.StatusSkip:
			skipped = append(skipped, c.ID)
		case c.InReplyTo == 0:
			comments++
		default:
			replies++

			if c.Resolve {
				resolved++
			}
		}
	}

	var sent []string
	if comments > 0 {
		sent = append(sent, humanize.Plural(comments, "comment"))
	}

	if replies > 0 {
		sent = append(sent, humanize.Plural(replies, "reply", "replies"))
	}

	out := "posted the review"
	if len(sent) > 0 {
		out = "posted " + strings.Join(sent, " and ")
	}

	var tail []string
	if resolved > 0 {
		tail = append(tail, fmt.Sprintf("resolved %d", resolved))
	}

	for _, id := range skipped {
		tail = append(tail, "skipped "+id)
	}

	if len(tail) > 0 {
		out += " · " + strings.Join(tail, " · ")
	}

	return out
}

func commentEndpoint(r *artifact.Review) string {
	return fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", r.Owner, r.Repo, r.Number)
}

func write(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
