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

	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/seen"
	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/commits"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/jjseen"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/threads"
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
	// Range is git's A..B, read as the commits B has and A does not; the base
	// argument carries it.
	Range
	// Revset is the jj revisions a revset names, which must be contiguous; the
	// base argument carries it.
	Revset
)

// The default base a branch is read against on each backend: the remote's
// main line, which is where the fork point lives.
const (
	gitBase = "origin/HEAD"
	jjBase  = "trunk()"
	headRev = "HEAD"
	diffCmd = "diff"
)

// Reasons a local review is refused.
var (
	// ErrNoChanges is a local review of nothing: a clean working copy, or a
	// branch that has not moved past its base.
	ErrNoChanges = errors.New("nothing differs to review")
	ErrRangeInJJ = errors.New("--range names a git range; under jj, name the change with -r <revset>")
	ErrRevsetGit = errors.New("-r names a jj revset; under git, name the change with --range A..B")
	ErrRange     = errors.New("a range is A..B")
	ErrHeads     = errors.New("the revset has more than one head, so no one revision is the change's tip")
)

// Open reads the checkout's change into everything the review screen reads:
// the diff, the artifact its notes live in, and the read marks already left.
func Open(ctx context.Context, root string, mode Mode, base string) (*get.Review, error) {
	abs, err := prepare(root)
	if err != nil {
		return nil, err
	}

	jj := vcs.GetOperations(abs).VCSType() == vcs.TypeJJ

	patch, tip, fork, err := diffFor(ctx, abs, jj, mode, base)
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

	// What the change claims is one more subprocess, and nothing it reads
	// depends on the commits, so it runs beside them rather than after.
	claimed := make(chan string, 1)
	go func() { claimed <- claimText(ctx, abs, jj, mode, base, fork) }()

	listed, changes, why := commitsOf(ctx, abs, jj, mode, base, tip, fork, d)
	claim := <-claimed

	seenPath := artifact.LocalSeenPath(abs)

	read, err := seen.Load(seenPath)
	if err != nil {
		return nil, fmt.Errorf("reading what has already been read: %w", err)
	}

	shared, err := share(ctx, abs, jj, mode, changes, d)
	if err != nil {
		return nil, err
	}

	return &get.Review{
		Review: review, Diff: d,
		About: threads.About{
			Title: title(review, jj, mode, base, listed, why),
			Body:  claim,
		},
		Read: read, SeenPath: seenPath, Shared: shared,
		Path: path, HeadSHA: tip,
		Work: abs, OnHead: true,
		Commits: listed, NoCommits: why,
	}, nil
}

// commitsOf is each commit or revision the change spans with the hunks it wrote,
// or why there are none. A working copy spans none. The fork is where diffFor
// found the change starts, empty where it did not look.
func commitsOf(ctx context.Context, root string, jj bool, mode Mode, base, tip, fork string, d *diff.Diff,
) ([]plan.Commit, []jjseen.Change, string) {
	var (
		listed  []plan.Commit
		changes []jjseen.Change
		err     error
	)

	switch mode {
	case Worktree:
		return nil, nil, ""
	case Revset:
		listed, changes, err = commits.JJChanges(ctx, root, base, d)
	case Branch, Range:
		if fork == "" {
			var ferr error
			if fork, ferr = forkOf(ctx, root, jj, mode, base); ferr != nil {
				return nil, nil, "finding where the change starts: " + ferr.Error()
			}
		}

		if jj {
			listed, changes, err = commits.JJChanges(ctx, root, fork+"..@", d)
		} else {
			listed, err = commits.Git(ctx, root, fork, tip, d)
		}
	}

	if err != nil {
		return nil, nil, "mapping hunks onto the commits: " + err.Error()
	}

	return listed, changes, ""
}

// share is jojo's record for the jj changes under review, which the screen imports as it opens and
// writes back to on every save: nil under git, where there are no change ids. A jj working
// copy is one change, @, whose diff is the review's own; a span brings its changes from commitsOf.
func share(ctx context.Context, root string, jj bool, mode Mode, changes []jjseen.Change, d *diff.Diff,
) (*jjseen.Marks, error) {
	if !jj {
		return nil, nil //nolint:nilnil // git has no change ids, which is no error
	}

	live := seen.Hunks(d)

	if mode == Worktree {
		id, err := run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "-r", "@", "-T", "change_id")
		if err != nil {
			return nil, fmt.Errorf("naming the working-copy change: %w", err)
		}

		changes = []jjseen.Change{{ID: strings.TrimSpace(id), Hunks: live}}
	}

	if len(changes) == 0 {
		return nil, nil //nolint:nilnil // a span whose commits could not be read shares nothing
	}

	marks, err := jjseen.Open(root, changes)
	if err != nil {
		return nil, fmt.Errorf("opening jojo's read marks: %w", err)
	}

	return marks, nil
}

// title names a range review by the range and what it spans, which is what a
// pull request's title says in its place.
func title(review *artifact.Review, jj bool, mode Mode, spec string, listed []plan.Commit, why string) string {
	if mode != Range && mode != Revset {
		return review.HeadRef
	}

	bin, unit := "git", "commit"
	if jj {
		bin, unit = "jj", "revision"
	}

	if why != "" {
		return bin + " " + spec
	}

	return bin + " " + spec + " · " + humanize.Plural(len(listed), unit)
}

// claimText is what the change says it is for: the working-copy change's
// description under jj, and the branch's own commit subjects under either
// backend. A git working copy carries no description of its own, which the
// question it feeds answers as "nothing claims anything" rather than
// borrowing HEAD's message, which is the base's intent, not the change's.
//
// The fork is where diffFor found a git branch leaves its base, which saves
// asking git again.
func claimText(ctx context.Context, root string, jj bool, mode Mode, base, fork string) string {
	if mode == Branch && base == "" {
		base = gitBase
		if jj {
			base = jjBase
		}
	}

	var (
		out string
		err error
	)

	switch {
	case jj && mode == Worktree:
		out, err = run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "-r", "@", "-T", "description")
	case mode == Revset:
		out, err = run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "-r", base, "-T", "description")
	case mode == Range:
		from, to, rerr := split(base)
		if rerr != nil {
			return ""
		}

		out, err = run(ctx, root, "git", "log", "--format=%B", from+".."+to)
	case jj:
		out, err = run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "-r", base+"..@", "-T", "description")
	case mode == Worktree:
		return ""
	default:
		if fork == "" {
			var ferr error
			if fork, ferr = mergeBase(ctx, root, jj, base); ferr != nil {
				return ""
			}
		}

		out, err = run(ctx, root, "git", "log", "--format=%B", fork+"..HEAD")
	}

	if err != nil {
		return ""
	}

	return strings.TrimSpace(out)
}

// Patch is the diff alone, for a caller with no screen to draw it in.
func Patch(ctx context.Context, root string, mode Mode, base string) ([]byte, error) {
	abs, err := prepare(root)
	if err != nil {
		return nil, err
	}

	patch, _, _, err := diffFor(ctx, abs, vcs.GetOperations(abs).VCSType() == vcs.TypeJJ, mode, base)

	return patch, err
}

// Reader reads a file as the reviewed side of the diff has it. Where the
// working copy is the post-image — a worktree diff on either backend, or jj's
// --to @ — the file off disk is the answer; a git branch's post-image is the
// committed tip.
func Reader(root string, mode Mode, tip string, jj bool) blob.Reader {
	if mode == Worktree || (jj && mode != Revset) {
		return blob.Reader{Work: root}
	}

	return blob.Reader{Work: root, SHA: tip}
}

// diffFor is the patch and the tip it was taken at. The tip is @ for jj —
// the working-copy change is itself a commit — and HEAD for git.
func diffFor(ctx context.Context, root string, jj bool, mode Mode, base string) ([]byte, string, string, error) {
	switch {
	case mode == Range && jj:
		return nil, "", "", ErrRangeInJJ
	case mode == Revset && !jj:
		return nil, "", "", ErrRevsetGit
	case mode == Range || mode == Revset:
		// A span names its own tip, so the working copy's is not asked for.
		return spanDiff(ctx, root, mode, base)
	}

	return headDiff(ctx, root, jj, mode, base)
}

// headDiff is the patch of the working copy or of a branch, its tip, and where
// a branch leaves its base.
func headDiff(ctx context.Context, root string, jj bool, mode Mode, base string) ([]byte, string, string, error) {
	bin, at := "git", headRev
	if jj {
		bin, at = "jj", "@"
	}

	tip, err := revision(ctx, root, jj, at)
	if err != nil {
		return nil, "", "", err
	}

	var fork string

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
		if fork, err = mergeBase(ctx, root, jj, base); err != nil {
			return nil, "", "", err
		}

		if jj {
			args = []string{diffCmd, "--git", "--no-pager", "--from", fork, "--to", "@"}
		} else {
			args = []string{diffCmd, fork, headRev, "--no-color", "--no-ext-diff"}
		}
	case Range, Revset:
		// Answered by spanDiff before the tip was read.
	}

	out, err := run(ctx, root, bin, args...)
	if err != nil {
		return nil, "", "", fmt.Errorf("reading the diff: %w", err)
	}

	return []byte(out), tip, fork, nil
}

// spanDiff is the cumulative patch of a git range or a jj revset, its tip, and
// where a git range starts.
func spanDiff(ctx context.Context, root string, mode Mode, spec string) ([]byte, string, string, error) {
	if mode == Revset {
		heads, err := run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "-r", "heads("+spec+")",
			"-T", `commit_id ++ "\n"`)
		if err != nil {
			return nil, "", "", fmt.Errorf("resolving %s: %w", spec, err)
		}

		tips := strings.Fields(heads)
		if len(tips) != 1 {
			return nil, "", "", fmt.Errorf("%s: %w", spec, ErrHeads)
		}

		out, err := run(ctx, root, "jj", diffCmd, "--git", "--no-pager", "-r", spec)
		if err != nil {
			return nil, "", "", fmt.Errorf("reading the diff: %w", err)
		}

		return []byte(out), tips[0], "", nil
	}

	fork, err := forkOf(ctx, root, false, mode, spec)
	if err != nil {
		return nil, "", "", err
	}

	_, to, err := split(spec)
	if err != nil {
		return nil, "", "", err
	}

	tip, err := revision(ctx, root, false, to)
	if err != nil {
		return nil, "", "", err
	}

	out, err := run(ctx, root, "git", diffCmd, fork, tip, "--no-color", "--no-ext-diff")
	if err != nil {
		return nil, "", "", fmt.Errorf("reading the diff: %w", err)
	}

	return []byte(out), tip, fork, nil
}

// forkOf is where a branch or a git range starts: the commit its two ends last
// shared, which is the old side of every hunk.
func forkOf(ctx context.Context, root string, jj bool, mode Mode, spec string) (string, error) {
	if mode != Range {
		return mergeBase(ctx, root, jj, spec)
	}

	from, to, err := split(spec)
	if err != nil {
		return "", err
	}

	out, err := run(ctx, root, "git", "merge-base", from, to)
	if err != nil {
		return "", fmt.Errorf("finding where %s starts: %w", spec, err)
	}

	return strings.TrimSpace(out), nil
}

// split reads A..B, an empty side standing for HEAD the way git's own does.
func split(spec string) (string, string, error) {
	from, to, ok := strings.Cut(spec, "..")
	if !ok || strings.HasPrefix(to, ".") {
		return "", "", fmt.Errorf("%w, not %q", ErrRange, spec)
	}

	if from == "" {
		from = headRev
	}

	if to == "" {
		to = headRev
	}

	return from, to, nil
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

	switch mode {
	case Worktree:
		return work, ""
	case Range, Revset:
		return "", ""
	case Branch:
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
	if mode == Range || mode == Revset {
		return fmt.Errorf("%w: %s changes nothing", ErrNoChanges, base)
	}

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
