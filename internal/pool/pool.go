// Package pool answers the diff the GitHub API refuses to serve: the merge
// base and the head are fetched into a local clone, and git diffs them. The
// clone is the reader's own checkout when it names the repository — a fetch
// moves objects only, so even a dirty tree is safe — and a slot in a small
// pool of bare clones under the OS temp directory otherwise, which the
// system reclaims on restart.
package pool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// slots is how many repositories the pool keeps between runs. Reaching for a
// fourth drops the least recently used.
const slots = 3

// graphDepth is how much history a commit-graph fetch asks for before the
// merge-base walk gives up and deepens.
const graphDepth = 300

const (
	baseRef = "refs/sl/base"
	headRef = "refs/sl/head"
)

// Diff is the pull request's patch: merge-base to head, which is what
// `gh pr diff` reports. base is the pull request's base branch name, head its
// head commit.
//
// work is an existing checkout to borrow, or empty for a pooled clone. The
// answer is the same either way; borrowing the checkout only skips the pool
// and makes a revisit cost an incremental fetch.
func Diff(ctx context.Context, work, host, owner, repo, base, head string) ([]byte, error) {
	dir := work
	if !namesRepo(ctx, dir, owner, repo) {
		var err error
		if dir, err = slot(ctx, host, owner, repo); err != nil {
			return nil, err
		}
	}

	mb, err := mergeBase(ctx, dir, base, head)
	if err != nil {
		return nil, err
	}

	// Full objects for just the two trees the diff reads: each fetch is a
	// shallow one-commit pull, which is the whole input and nothing older.
	for _, sha := range []string{mb, head} {
		if err := git(ctx, dir, "fetch", "--no-filter", "--depth=1", "origin", sha); err != nil {
			return nil, fmt.Errorf("fetching %s for the diff: %w", sha[:7], err)
		}
	}

	patch, err := output(ctx, dir, "diff", mb+"..."+head)
	if err != nil {
		return nil, fmt.Errorf("diffing %s...%s: %w", mb[:7], head[:7], err)
	}

	return []byte(patch), nil
}

// namesRepo reports whether dir is a git checkout of owner/repo, which is
// what borrowing it for a fetch is allowed to assume.
func namesRepo(ctx context.Context, dir, owner, repo string) bool {
	if dir == "" {
		return false
	}

	url, err := output(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return false
	}

	return strings.Contains(url, "/"+owner+"/"+repo)
}

// mergeBase finds the commit a three-dot diff starts from. The commit-graph
// fetches carry no blobs, so this is cheap metadata; if a very long pull
// request outruns the depth it is fetched once more, deeper.
func mergeBase(ctx context.Context, dir, base, head string) (string, error) {
	depth := graphDepth

	for range 2 {
		if err := git(ctx, dir, "fetch", "--filter=blob:none",
			fmt.Sprintf("--depth=%d", depth), "origin",
			base+":"+baseRef, head+":"+headRef,
		); err != nil {
			return "", fmt.Errorf("fetching the pull request's history: %w", err)
		}

		mb, err := output(ctx, dir, "merge-base", baseRef, headRef)
		if err == nil {
			return strings.TrimSpace(mb), nil
		}

		if err := git(ctx, dir, "fetch", "--filter=blob:none",
			fmt.Sprintf("--deepen=%d", depth), "origin", baseRef, headRef,
		); err != nil {
			return "", fmt.Errorf("deepening the pull request's history: %w", err)
		}
	}

	mb, err := output(ctx, dir, "merge-base", baseRef, headRef)
	if err != nil {
		return "", fmt.Errorf("finding the merge base: %w", err)
	}

	return strings.TrimSpace(mb), nil
}

// slot is the pool's clone for one repository, created on first use and
// freshened by the caller's fetches. Making a slot prunes the pool to slots.
func slot(ctx context.Context, host, owner, repo string) (string, error) {
	root := filepath.Join(os.TempDir(), "second-look", "repos")
	dir := filepath.Join(root, host+"-"+owner+"-"+repo)

	if _, err := output(ctx, dir, "rev-parse", "--git-dir"); err == nil {
		// Reuse counts as use: the mtime is what pruning reads.
		_ = os.Chtimes(dir, time.Now(), time.Now()) //nolint:errcheck // a mtime the OS ignores costs nothing

		return dir, nil
	}

	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", fmt.Errorf("creating the clone pool: %w", err)
	}

	// --bare: nothing here is a worktree. --filter=blob:none keeps the clone
	// to commits and trees; the caller fetches the blobs its diff needs.
	if err := git(ctx, "",
		"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential",
		"clone", "--bare", "--filter=blob:none",
		"https://"+host+"/"+owner+"/"+repo+".git", dir,
	); err != nil {
		return "", fmt.Errorf("cloning %s/%s for the diff: %w", owner, repo, err)
	}

	prune(root)

	return dir, nil
}

// prune drops the oldest slots past slots, by directory mtime.
func prune(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	aged := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			aged = append(aged, e)
		}
	}

	if len(aged) <= slots {
		return
	}

	sort.Slice(aged, func(i, j int) bool {
		iw, _ := aged[i].Info()
		jw, _ := aged[j].Info()

		return iw.ModTime().After(jw.ModTime())
	})

	for _, e := range aged[slots:] {
		_ = os.RemoveAll(filepath.Join(root, e.Name())) //nolint:errcheck // a slot that will not die is swept next run
	}
}

func git(ctx context.Context, dir string, args ...string) error {
	_, err := output(ctx, dir, args...)

	return err
}

func output(ctx context.Context, dir string, args ...string) (string, error) {
	argv := append([]string{}, args...)
	if dir != "" {
		argv = append([]string{"-C", dir}, argv...)
	}

	//nolint:gosec // git's argv is built here from repository coordinates
	out, err := exec.CommandContext(ctx, "git", argv...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", args[0], err, out)
	}

	return string(out), nil
}
