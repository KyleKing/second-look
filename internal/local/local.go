// Package local opens a review of the checkout's own change rather than a
// pull request's: the working copy against its head, or the branch against
// where it left its base. There is no forge in the loop, so a comment written
// here is a note that never leaves .second-look.
package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/seen"
)

// Mode is which of a checkout's own diffs is under review.
type Mode int

const (
	// Worktree is everything the head does not carry: git's staged and
	// unstaged work against HEAD, or the jj working-copy change, which has no
	// committed half to exclude.
	Worktree Mode = iota
	// Branch is every commit since the branch left its base.
	Branch
)

// The default base a branch is read against on each backend: the remote's
// main line, which is where the fork point lives.
const (
	gitBase = "origin/HEAD"
	jjBase  = "trunk()"
	headRev = "HEAD"
	diffCmd = "diff"
)

// ErrNoChanges is a local review of nothing: a clean working copy, or a
// branch that has not moved past its base.
var ErrNoChanges = errors.New("nothing differs to review")

// Open reads the checkout's change into everything the review screen reads:
// the diff, the artifact its notes live in, and the read marks already left.
func Open(ctx context.Context, root string, mode Mode, base string) (*get.Review, error) {
	abs, err := prepare(root)
	if err != nil {
		return nil, err
	}

	jj := vcs.GetOperations(abs).VCSType() == vcs.TypeJJ

	patch, tip, err := diffFor(ctx, abs, jj, mode, base)
	if err != nil {
		return nil, err
	}

	d := diff.Parse(patch)
	if len(d.Files) == 0 {
		return nil, empty(mode, base, jj)
	}

	review, path, err := stage(ctx, abs, mode, base, jj, tip)
	if err != nil {
		return nil, err
	}

	seenPath := seen.LocalPath(abs)

	read, err := seen.Load(seenPath)
	if err != nil {
		return nil, fmt.Errorf("reading what has already been read: %w", err)
	}

	return &get.Review{
		Review: review, Diff: d,
		Read: read, SeenPath: seenPath,
		Path: path, HeadSHA: tip,
		Work: abs, OnHead: true,
	}, nil
}

// Patch is the diff alone, for a caller with no screen to draw it in.
func Patch(ctx context.Context, root string, mode Mode, base string) ([]byte, error) {
	abs, err := prepare(root)
	if err != nil {
		return nil, err
	}

	patch, _, err := diffFor(ctx, abs, vcs.GetOperations(abs).VCSType() == vcs.TypeJJ, mode, base)

	return patch, err
}

// Base is the commit the diff's old side numbers its lines from, which is the
// revision blame is asked at. It is computed rather than remembered because a
// blame pass runs after the screen is already up.
func Base(ctx context.Context, root string, mode Mode, base string) (string, error) {
	abs, err := prepare(root)
	if err != nil {
		return "", err
	}

	jj := vcs.GetOperations(abs).VCSType() == vcs.TypeJJ
	if mode == Branch {
		return mergeBase(ctx, abs, jj, base)
	}

	if jj {
		return revision(ctx, abs, jj, "@-")
	}

	return revision(ctx, abs, jj, headRev)
}

// Reader reads a file as the reviewed side of the diff has it. Where the
// working copy is the post-image — a worktree diff on either backend, or jj's
// --to @ — the file off disk is the answer; a git branch's post-image is the
// committed tip.
func Reader(root string, mode Mode, tip string, jj bool) blob.Reader {
	if mode == Worktree || jj {
		return blob.Reader{Work: root}
	}

	return blob.Reader{Work: root, SHA: tip}
}

// diffFor is the patch and the tip it was taken at. The tip is @ for jj —
// the working-copy change is itself a commit — and HEAD for git.
func diffFor(ctx context.Context, root string, jj bool, mode Mode, base string) ([]byte, string, error) {
	bin, at := "git", headRev
	if jj {
		bin, at = "jj", "@"
	}

	tip, err := revision(ctx, root, jj, at)
	if err != nil {
		return nil, "", err
	}

	// --no-ext-diff because a configured diff.external would render something
	// that is not a patch, which is all diff.Parse can read.
	var args []string

	switch mode {
	case Worktree:
		if jj {
			args = []string{diffCmd, "--git", "--no-pager"}
		} else {
			args = []string{diffCmd, headRev, "--no-color", "--no-ext-diff"}
		}
	case Branch:
		fork, err := mergeBase(ctx, root, jj, base)
		if err != nil {
			return nil, "", err
		}

		if jj {
			args = []string{diffCmd, "--git", "--no-pager", "--from", fork, "--to", "@"}
		} else {
			args = []string{diffCmd, fork, headRev, "--no-color", "--no-ext-diff"}
		}
	}

	out, err := run(ctx, root, bin, args...)
	if err != nil {
		return nil, "", fmt.Errorf("reading the diff: %w", err)
	}

	return []byte(out), tip, nil
}

// mergeBase is the commit the branch and its base last shared, asked of the
// backend: git knows it as merge-base, jj as the fork point of what is not on
// the trunk.
func mergeBase(ctx context.Context, root string, jj bool, base string) (string, error) {
	if base == "" {
		base = gitBase
		if jj {
			base = jjBase
		}
	}

	if jj {
		return revision(ctx, root, jj, "fork_point("+base+"..@)")
	}

	out, err := run(ctx, root, "git", "merge-base", "HEAD", base)
	if err != nil {
		return "", fmt.Errorf("finding where the branch left %s: %w", base, err)
	}

	return strings.TrimSpace(out), nil
}

// revision resolves a name to a commit: Jj answers revsets, git answers refs.
func revision(ctx context.Context, root string, jj bool, rev string) (string, error) {
	if jj {
		out, err := run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "-r", rev, "-T", "commit_id")
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", rev, err)
		}

		return strings.TrimSpace(out), nil
	}

	out, err := run(ctx, root, "git", "rev-parse", rev)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", rev, err)
	}

	return strings.TrimSpace(out), nil
}

// stage is the artifact the local review's comments live in, created on the
// first open and restamped on each: a local diff moves under its notes the
// way a pushed head moves under a pull request's.
func stage(ctx context.Context, root string, mode Mode, base string, jj bool, tip string,
) (*artifact.Review, string, error) {
	path := artifact.LocalPath(root)

	review, err := artifact.LoadOrNew(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading the local review: %w", err)
	}

	review.Local = true
	review.Number = 0
	review.HeadSHA = tip
	review.HeadRef, review.BaseRef = refs(ctx, root, mode, base, jj)

	if review.Repo == "" {
		review.Owner, review.Repo = repoName(ctx, root)
	}

	if err := artifact.Save(path, review); err != nil {
		return nil, "", fmt.Errorf("writing the local review: %w", err)
	}

	return review, path, nil
}

// refs is what the title calls a local review: "worktree" (or jj's @) for the
// uncommitted read, and the branch against its base for the other.
func refs(ctx context.Context, root string, mode Mode, base string, jj bool) (string, string) {
	work := "worktree"
	if jj {
		work = "@"
	}
	if mode == Worktree {
		return work, ""
	}

	head, err := vcs.GetOperations(root).GetCurrentBranch(ctx, root)
	if err != nil || head == "" {
		head = "HEAD"
		if jj {
			head = "@"
		}
	}

	if base == "" {
		base = gitBase
		if jj {
			base = jjBase
		}
	}

	return head, base
}

// repoName is the owner/name a repository's remote names it, and its
// directory where no remote does — a checkout with no remote still has a name
// to title a review with.
func repoName(ctx context.Context, root string) (string, string) {
	full, err := get.Repository(ctx, root)
	if err != nil {
		return "", filepath.Base(root)
	}

	owner, name, _ := strings.Cut(full, "/")

	return owner, name
}

func prepare(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("locating %s: %w", root, err)
	}

	if !vcs.IsRepo(abs) {
		return "", fmt.Errorf("%s: %w", abs, get.ErrNotARepo)
	}

	return abs, nil
}

// empty is the nothing-to-review answer, which names what was asked rather
// than just that it came back clean.
func empty(mode Mode, base string, jj bool) error {
	if mode != Branch {
		return fmt.Errorf("%w: the working copy is clean", ErrNoChanges)
	}

	if base == "" {
		base = gitBase
		if jj {
			base = jjBase
		}
	}

	return fmt.Errorf("%w: nothing past %s", ErrNoChanges, base)
}

func run(ctx context.Context, root, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- constants, a revset, and a ref
	cmd.Dir = root

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return string(out), nil
}
