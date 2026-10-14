package local_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/local"
)

// repo is a git checkout with one commit on main holding a.txt, which is the
// least a local review can stand on.
func repo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	git(t, dir, "init", "--quiet", "--initial-branch", "main")
	write(t, dir, "a.txt", "one\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "base")

	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- a fixture's own constants
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorktree(t *testing.T) {
	t.Parallel()
	dir := repo(t)
	write(t, dir, "a.txt", "one\ntwo\n")

	opened, err := local.Open(t.Context(), dir, local.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}

	if !opened.Review.Local {
		t.Error("the review is not marked local")
	}
	if len(opened.Diff.Files) != 1 || opened.Diff.Files[0].NewPath != "a.txt" {
		t.Fatalf("the diff holds %v, want a.txt alone", opened.Diff.Files)
	}
	if opened.HeadSHA != git(t, dir, "rev-parse", "HEAD") {
		t.Error("the review is not stamped at the head it was read against")
	}
	if opened.Path != artifact.LocalPath(dir) {
		t.Errorf("the artifact is at %s, not the checkout's local.toml", opened.Path)
	}
	if !opened.OnHead {
		t.Error("the working copy is where the change under review stands")
	}
	if opened.Review.HeadRef != "worktree" {
		t.Errorf("the review is titled by %q rather than what is diffed", opened.Review.HeadRef)
	}

	if _, err := artifact.Load(opened.Path); err != nil {
		t.Fatalf("the artifact on disk does not revalidate: %v", err)
	}
}

func TestWorktreeClean(t *testing.T) {
	t.Parallel()
	if _, err := local.Open(t.Context(), repo(t), local.Worktree, ""); !errors.Is(err, local.ErrNoChanges) {
		t.Fatalf("a clean working copy answers %v, not ErrNoChanges", err)
	}
}

func TestBranch(t *testing.T) {
	t.Parallel()

	dir := repo(t)

	git(t, dir, "checkout", "--quiet", "-b", "feat")
	write(t, dir, "b.txt", "new\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "work")
	write(t, dir, "a.txt", "one\nuncommitted\n")

	opened, err := local.Open(t.Context(), dir, local.Branch, "main")
	if err != nil {
		t.Fatal(err)
	}

	if len(opened.Diff.Files) != 1 || opened.Diff.Files[0].NewPath != "b.txt" {
		t.Fatalf("the branch diff holds %v; the committed b.txt belongs, the uncommitted a.txt does not",
			opened.Diff.Files)
	}
	if opened.Review.HeadRef != "feat" || opened.Review.BaseRef != "main" {
		t.Errorf("the review is titled %s vs %s", opened.Review.HeadRef, opened.Review.BaseRef)
	}
}

func TestBranchNoMove(t *testing.T) {
	t.Parallel()
	if _, err := local.Open(t.Context(), repo(t), local.Branch, "main"); !errors.Is(err, local.ErrNoChanges) {
		t.Fatalf("a branch still on its base answers %v, not ErrNoChanges", err)
	}
}

func TestNotARepo(t *testing.T) {
	t.Parallel()
	if _, err := local.Open(t.Context(), t.TempDir(), local.Worktree, ""); !errors.Is(err, get.ErrNotARepo) {
		t.Fatalf("a directory outside any repository answers %v, not ErrNotARepo", err)
	}
}

// TestNotesStay is the whole point of the artifact: comments written on a
// local review are read back by the next, and are nobody else's business.
func TestNotesStay(t *testing.T) {
	t.Parallel()

	dir := repo(t)
	write(t, dir, "a.txt", "one\ntwo\n")

	opened, err := local.Open(t.Context(), dir, local.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}

	opened.Review.Upsert(artifact.Comment{
		ID: "n1", Path: "a.txt", Line: 2, Side: artifact.SideRight,
		Body: "remember why this is here", Status: artifact.StatusReady,
	})

	if err := artifact.Save(opened.Path, opened.Review); err != nil {
		t.Fatal(err)
	}

	again, err := local.Open(t.Context(), dir, local.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(again.Review.Comments) != 1 || again.Review.Comments[0].Body != "remember why this is here" {
		t.Fatalf("the note did not survive a reopen: %v", again.Review.Comments)
	}
	if !again.Review.Local {
		t.Error("a reopened local review lost its local marking")
	}
}

// TestWorktreeJJ is the jj claim: the working-copy change is the diff, and a
// file the change adds is in it, which a colocated `git diff HEAD` cannot
// see because jj tracks it without staging it.
func TestWorktreeJJ(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}

	dir := t.TempDir()
	run(t, dir, "jj", "git", "init", "--no-pager")
	write(t, dir, "a.txt", "one\n")
	run(t, dir, "jj", "describe", "-m", "base", "--no-pager")
	run(t, dir, "jj", "new", "--no-pager")
	write(t, dir, "b.txt", "brand new\n")

	opened, err := local.Open(t.Context(), dir, local.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(opened.Diff.Files) != 1 || opened.Diff.Files[0].NewPath != "b.txt" {
		t.Fatalf("the jj diff holds %v, want the new file b.txt", opened.Diff.Files)
	}
	if opened.Review.HeadRef != "@" {
		t.Errorf("the jj review is titled by %q rather than the working copy", opened.Review.HeadRef)
	}
}

func run(t *testing.T, dir, bin string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), bin, args...) // #nosec G204 -- a fixture's own constants
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", bin, args, err, out)
	}
}

func TestPatch(t *testing.T) {
	t.Parallel()

	dir := repo(t)
	write(t, dir, "a.txt", "one\ntwo\n")

	patch, err := local.Patch(t.Context(), dir, local.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patch), "+two") {
		t.Errorf("the patch does not carry the uncommitted line: %s", patch)
	}

	if _, err := os.Stat(artifact.LocalPath(dir)); err == nil {
		t.Error("a patch read wrote a review; a pipe should touch nothing")
	}
}
