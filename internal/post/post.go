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
	"github.com/kyleking/second-look/internal/prepared"
)

// rootOf is the checkout or state directory a prepared review is staged under.
func rootOf(path string) string { return filepath.Dir(filepath.Dir(path)) }

// ErrHeadMoved reports a pull request whose head advanced since the review
// was prepared.
var ErrHeadMoved = errors.New("the pull request has new commits")

// Poster sends one request. The gh CLI is the only implementation that ships;
// a test supplies its own.
type Poster interface {
	Post(ctx context.Context, endpoint string, body []byte) error
}

type ghPoster struct{}

// GH posts by shelling out to `gh api`.
//
//nolint:ireturn // Poster is the seam a test replaces; concrete would remove it
func GH() Poster { return ghPoster{} }

func (ghPoster) Post(ctx context.Context, endpoint string, body []byte) error {
	//nolint:gosec // the endpoint is built from the artifact's own owner, repo, and number
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "POST", endpoint, "--input", "-")
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh api POST %s: %w", endpoint, err)
	}

	return nil
}

// Guard compares every comment against the pull request's current diff before
// anything is sent. A comment whose line moved would land on whatever now
// sits there, which is worse than not posting it.
//
// The directory gh runs in is dir, and remoteRepo names the repository when that
// directory does not, which is how a review prepared with no checkout is
// guarded from anywhere.
func Guard(ctx context.Context, dir, remoteRepo string, r *artifact.Review) error {
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
		return fmt.Errorf("reading the current diff: %w", err)
	}

	if err := artifact.Verify(r.Comments, diff.Parse(patch)); err != nil {
		return fmt.Errorf("nothing was posted:\n%w", err)
	}

	return nil
}

// Run posts an already-guarded review, then its replies, and removes the
// artifact file at path once every request succeeds.
func Run(ctx context.Context, p Poster, path string, r *artifact.Review, out io.Writer) error {
	payload, replies, err := r.Payload()
	if err != nil {
		return fmt.Errorf("building the payload: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding the review: %w", err)
	}

	endpoint := reviewEndpoint(r)

	//nolint:wrapcheck // the caller reports the raw gh failure verbatim
	if err := p.Post(ctx, endpoint, body); err != nil {
		return err
	}

	if err := write(out, "posted "+endpoint+"\n"); err != nil {
		return err
	}

	if err := postReplies(ctx, p, r, replies); err != nil {
		return err
	}

	// GitHub is the source of truth from here, and a prepared review left on
	// disk would post a second copy of itself if anyone ran post again. What was
	// cached against the head goes with it, and the read marks stay: a second
	// pass over the same pull request still knows which hunks were read.
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("the review posted; removing %s: %w", path, err)
	}

	if _, err := prepared.Sweep(rootOf(path)); err != nil {
		return fmt.Errorf("the review posted; clearing its caches: %w", err)
	}

	return write(out, fmt.Sprintf("removed the staged review for %s/%s#%d\n", r.Owner, r.Repo, r.Number))
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

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding the comment: %w", err)
	}

	endpoint := commentEndpoint(r)
	if c.InReplyTo != 0 {
		endpoint = replyEndpoint(r, c.InReplyTo)
	}

	if c.InReplyTo == 0 {
		body, err = withCommit(body, r.HeadSHA)
		if err != nil {
			return err
		}
	}

	//nolint:wrapcheck // the caller reports the raw gh failure verbatim
	if err := p.Post(ctx, endpoint, body); err != nil {
		return err
	}

	if err := write(out, "posted "+endpoint+"\n"); err != nil {
		return err
	}

	r.Remove(id)

	if err := artifact.Save(path, r); err != nil {
		return fmt.Errorf("the comment posted; rewriting %s: %w", path, err)
	}

	return write(out, fmt.Sprintf("removed %s from the staged review for %s/%s#%d\n", id, r.Owner, r.Repo, r.Number))
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

// DryRun prints what Run would send without sending it.
func DryRun(out io.Writer, r *artifact.Review) error {
	payload, replies, err := r.Payload()
	if err != nil {
		return fmt.Errorf("building the payload: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding the review: %w", err)
	}

	if err := write(out, fmt.Sprintf("POST %s\n%s\n", reviewEndpoint(r), body)); err != nil {
		return err
	}

	for _, reply := range replies {
		line := fmt.Sprintf("POST %s\n", replyEndpoint(r, reply.InReplyTo))
		if err := write(out, line); err != nil {
			return err
		}
	}

	return nil
}

// postReplies runs after the review is already posted, which is why a failure
// here is reported with that fact rather than retried.
func postReplies(ctx context.Context, p Poster, r *artifact.Review, replies []artifact.ReplyPayload) error {
	for _, reply := range replies {
		rb, err := json.Marshal(reply)
		if err != nil {
			return fmt.Errorf("encoding a reply: %w", err)
		}

		if err := p.Post(ctx, replyEndpoint(r, reply.InReplyTo), rb); err != nil {
			return fmt.Errorf("the review posted but a reply did not, and the prepared review is"+
				" still on disk; posting it again would post the review twice: %w", err)
		}
	}

	return nil
}

func reviewEndpoint(r *artifact.Review) string {
	return fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", r.Owner, r.Repo, r.Number)
}

func commentEndpoint(r *artifact.Review) string {
	return fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", r.Owner, r.Repo, r.Number)
}

func replyEndpoint(r *artifact.Review, commentID int64) string {
	return fmt.Sprintf("/repos/%s/%s/pulls/%d/comments/%d/replies", r.Owner, r.Repo, r.Number, commentID)
}

func write(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
