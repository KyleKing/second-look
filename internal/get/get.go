// Package get prepares a review: it reads the pull request, moves the working
// copy onto its head, writes the artifact, and caches the diff.
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

// Run prepares the review for a pull request in the checkout at root.
func Run(ctx context.Context, out io.Writer, root string, number int) error {
	if !vcs.IsRepo(root) {
		return fmt.Errorf("%s: %w", root, ErrNotARepo)
	}

	repoID, err := identify(ctx, root)
	if err != nil {
		return err
	}

	pr, err := github.GetPR(ctx, root, number)
	if err != nil {
		return fmt.Errorf("reading pull request #%d: %w", number, err)
	}
	if pr.HeadSHA == "" {
		return fmt.Errorf("#%d: %w", number, ErrNoHeadSHA)
	}

	if err := checkout(ctx, out, root, pr); err != nil {
		return err
	}

	patch, err := github.PRDiff(ctx, root, number)
	if err != nil {
		return fmt.Errorf("caching the diff: %w", err)
	}
	if err := artifact.SaveDiff(root, pr.HeadSHA, patch); err != nil {
		return fmt.Errorf("caching the diff: %w", err)
	}

	return writeReview(out, root, repoID, pr)
}

// repo names the repository a review is filed against.
type repo struct {
	owner string
	name  string
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
		return fmt.Errorf("%w (%d file(s))", ErrDirtyTree, n)
	}

	return nil
}

// writeReview creates the artifact, or moves an existing one onto the new head
// and says how many comments came with it.
func writeReview(out io.Writer, root string, id repo, pr *forge.PullRequest) error {
	path := artifact.Path(root, pr.Number)

	review, err := artifact.LoadOrNew(path)
	if err != nil {
		return fmt.Errorf("reading the prepared review: %w", err)
	}

	moved := review.HeadSHA != "" && review.HeadSHA != pr.HeadSHA

	review.Version = artifact.SchemaVersion
	review.Host = "github.com"
	review.Owner = id.owner
	review.Repo = id.name
	review.Number = pr.Number
	review.HeadSHA = pr.HeadSHA

	if err := artifact.Save(path, review); err != nil {
		return fmt.Errorf("writing the prepared review: %w", err)
	}

	if moved {
		return say(out, fmt.Sprintf(
			"%s moved to %s; %d staged comment(s) came with it and are re-checked on post\n",
			path, short(pr.HeadSHA), len(review.Comments),
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
