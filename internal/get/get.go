// Package get prepares a review: it reads the pull request, writes the
// artifact, and caches the diff. Inside a checkout of the repository it also
// moves the working copy onto the pull request head.
package get

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kyleking/aragonite/forge"
	"github.com/kyleking/aragonite/forge/github"
	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/threads"
)

// Reasons get stops. Each names something only the person at the keyboard can
// resolve, so none of them is retried.
var (
	ErrDirtyTree = errors.New("the working tree has uncommitted changes; commit or stash them first")
	ErrHeadMoved = errors.New("the checkout did not land on the pull request head")
	ErrNoHeadSHA = errors.New("the pull request reported no head commit")
	ErrNoRemote  = errors.New("no owner/repo could be read from the remote")
	ErrNotARepo  = errors.New("not a git or jj repository")
)

// Run prepares the review for a pull request.
//
// A target with a checkout has its working copy moved onto the pull request
// head, which is what makes reading around the change and running it possible.
// A detached target moves nothing: everything the review itself needs comes off
// the API, and there is no tree to move.
func Run(ctx context.Context, out io.Writer, t Target) error {
	pr, err := github.GetPR(ctx, t.Dir(), t.Remote(), t.Number)
	if err != nil {
		return fmt.Errorf("reading pull request #%d: %w", t.Number, err)
	}
	if pr.HeadSHA == "" {
		return fmt.Errorf("#%d: %w", t.Number, ErrNoHeadSHA)
	}

	if !t.Detached() {
		if err := checkout(ctx, out, t.Work, pr); err != nil {
			return err
		}
	}

	patch, err := github.PRDiff(ctx, t.Dir(), t.Remote(), t.Number)
	if err != nil {
		return fmt.Errorf("caching the diff: %w", err)
	}
	if err := artifact.SaveDiff(t.Store, pr.HeadSHA, patch); err != nil {
		return fmt.Errorf("caching the diff: %w", err)
	}

	if err := cacheThreads(ctx, out, t, pr.HeadSHA); err != nil {
		return err
	}

	previous := headOf(t.Store, t.Number)
	if err := writeReview(out, t, pr); err != nil {
		return err
	}

	return carryRead(out, t.Store, t.Number, previous, pr.HeadSHA)
}

// cacheThreads reads the conversations already open on the pull request, so a
// second pass can answer them. Every run refreshes them, which is what makes
// get the way to pick up what was said since.
func cacheThreads(ctx context.Context, out io.Writer, t Target, sha string) error {
	open, err := threads.Fetch(ctx, t.Dir(), t.Owner, t.Repo, t.Number)
	if err != nil {
		//nolint:wrapcheck // Fetch's own error already names the pull request
		return err
	}

	if err := artifact.SaveThreads(t.Store, sha, open); err != nil {
		return fmt.Errorf("caching the review threads: %w", err)
	}

	if len(open) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(out, humanize.Plural(len(open), "open review thread")); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

// headOf is the commit the prepared review was last staged against, or empty
// when there is no review yet.
func headOf(root string, number int) string {
	review, err := artifact.Load(artifact.Path(root, number))
	if err != nil {
		return ""
	}

	return review.HeadSHA
}

// carryRead reports how much of the new head was already read, and prunes marks
// for hunks the new diff no longer carries. Nothing is moved: a mark is stored
// against a hunk's content, so it survives a new head on its own.
func carryRead(out io.Writer, root string, number int, oldSHA, newSHA string) error {
	if oldSHA == "" || oldSHA == newSHA {
		return nil
	}

	path := seen.Path(root, number)

	set, err := seen.Load(path)
	if err != nil {
		return fmt.Errorf("reading what has already been read: %w", err)
	}

	newPatch, err := artifact.LoadDiff(root, newSHA)
	if err != nil {
		return fmt.Errorf("reading the cached diff: %w", err)
	}

	current := diff.Parse(newPatch)
	carried := seen.Carry(set, current)

	if err := seen.Save(path, set, seen.Hunks(current)); err != nil {
		return fmt.Errorf("writing what has been read: %w", err)
	}

	total := len(seen.Hunks(current))
	if carried == 0 || total == 0 {
		return nil
	}

	return say(out, fmt.Sprintf("%d of %s were already read\n", carried, humanize.Plural(total, "hunk")))
}

// repo names the repository a review is filed against.
type repo struct {
	owner string
	name  string
}

// Repository is the owner/name this checkout files reviews against, which is
// what says whether a conversation on another repository's pull request can be
// answered from here.
func Repository(ctx context.Context, root string) (string, error) {
	if !vcs.IsRepo(root) {
		return "", notARepo(root)
	}

	id, err := identify(ctx, root)
	if err != nil {
		return "", err
	}

	return id.owner + "/" + id.name, nil
}

func notARepo(root string) error {
	return fmt.Errorf("%s: %w", root, ErrNotARepo)
}

// identify reads the repository off the remote rather than off the pull
// request's URL, because a fork's pull request still belongs to the upstream
// repository the review is filed against.
func identify(ctx context.Context, root string) (repo, error) {
	remote, err := vcs.GetOperations(root).GetRemoteURL(ctx, root)
	if err != nil {
		return repo{}, fmt.Errorf("reading the remote: %w", err)
	}

	owner, name, found := strings.Cut(vcs.ExtractRepoPath(remote), "/")
	if !found || owner == "" || name == "" {
		return repo{}, fmt.Errorf("%q: %w", remote, ErrNoRemote)
	}

	return repo{owner: owner, name: name}, nil
}

// checkout moves the working copy onto the pull request head.
//
// Already being on the head never blocks, however dirty the tree: refusing to
// review a branch you already have because you have unstaged edits would be
// wrong. Moving the tree is the case that needs a clean one.
func checkout(ctx context.Context, out io.Writer, root string, pr *forge.PullRequest) error {
	head, err := vcs.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("reading the checkout: %w", err)
	}
	if head == pr.HeadSHA {
		return nil
	}

	ops := vcs.GetOperations(root)

	branch, err := ops.GetCurrentBranch(ctx, root)
	if err != nil {
		return fmt.Errorf("reading the current branch: %w", err)
	}

	if branch == pr.HeadRef {
		if err := vcs.PullFastForward(ctx, root); err != nil {
			return fmt.Errorf("catching up to %s: %w", pr.HeadRef, err)
		}
	} else {
		if err := requireCleanTree(ctx, ops, root); err != nil {
			return err
		}
		if _, err := github.CheckoutPR(ctx, root, pr.Number); err != nil {
			return fmt.Errorf("checking out #%d: %w", pr.Number, err)
		}
	}

	head, err = vcs.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("reading the checkout: %w", err)
	}
	if head != pr.HeadSHA {
		return fmt.Errorf("%w: at %s, expected %s", ErrHeadMoved, short(head), short(pr.HeadSHA))
	}

	return say(out, fmt.Sprintf("checked out %s at %s\n", pr.HeadRef, short(pr.HeadSHA)))
}

// requireCleanTree guards a checkout that has to move the working copy. A jj
// working copy is itself a commit, so there is nothing uncommitted to clobber
// and nothing to guard.
func requireCleanTree(ctx context.Context, ops vcs.StatusReader, root string) error {
	if ops.VCSType() == vcs.TypeJJ {
		return nil
	}

	summary, err := ops.GetRepoSummary(ctx, root)
	if err != nil {
		return fmt.Errorf("reading the working tree: %w", err)
	}

	if n := summary.UncommittedCount(); n > 0 {
		return fmt.Errorf("%w (%s)", ErrDirtyTree, humanize.Plural(n, "file"))
	}

	return nil
}

// writeReview creates the artifact, or moves an existing one onto the new head
// and says how many comments came with it.
func writeReview(out io.Writer, t Target, pr *forge.PullRequest) error {
	path := artifact.Path(t.Store, pr.Number)

	review, err := artifact.LoadOrNew(path)
	if err != nil {
		return fmt.Errorf("reading the prepared review: %w", err)
	}

	moved := review.HeadSHA != "" && review.HeadSHA != pr.HeadSHA

	review.Version = artifact.SchemaVersion
	review.Host = Host
	review.Owner = t.Owner
	review.Repo = t.Repo
	review.Number = pr.Number
	review.HeadSHA = pr.HeadSHA

	if err := artifact.Save(path, review); err != nil {
		return fmt.Errorf("writing the prepared review: %w", err)
	}

	if moved {
		return say(out, fmt.Sprintf(
			"%s moved to %s; %s came with it and are re-checked on post\n",
			path, short(pr.HeadSHA), humanize.Plural(len(review.Comments), "staged comment"),
		))
	}

	return say(out, fmt.Sprintf("%s ready at %s\n", path, short(pr.HeadSHA)))
}

func short(sha string) string {
	const n = 7
	if len(sha) <= n {
		return sha
	}

	return sha[:n]
}

func say(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
