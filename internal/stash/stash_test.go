package stash_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/stash"
)

// TestPushParksEverythingAndPopBringsItBack checks the promise the prompt makes:
// the tree is clean enough to move, and the quoted command is the one that
// actually restores both the edit and the file git does not track yet.
func TestPushParksEverythingAndPopBringsItBack(t *testing.T) {
	t.Parallel()

	dir, git := repo(t)

	write(t, filepath.Join(dir, "tracked.txt"), "edited\n")
	write(t, filepath.Join(dir, "untracked.txt"), "new\n")

	if err := stash.Push(t.Context(), dir, "second-look: before reviewing #7"); err != nil {
		t.Fatalf("parking the work: %v", err)
	}

	if status := git("status", "--porcelain"); status != "" {
		t.Errorf("the tree is still dirty: %q", status)
	}

	if list := git("stash", "list"); !strings.Contains(list, "before reviewing #7") {
		t.Errorf("want the stash labeled, got %q", list)
	}

	git(strings.Fields(stash.Restore)[1:]...)

	if got := read(t, filepath.Join(dir, "tracked.txt")); got != "edited\n" {
		t.Errorf("the edit came back as %q", got)
	}

	if got := read(t, filepath.Join(dir, "untracked.txt")); got != "new\n" {
		t.Errorf("the untracked file came back as %q", got)
	}
}

func TestPushFailsOutsideARepository(t *testing.T) {
	t.Parallel()

	if err := stash.Push(t.Context(), t.TempDir(), "nowhere"); err == nil {
		t.Fatal("want a failure outside a repository")
	}
}

// repo is a git repository with one tracked file committed, and a runner for
// reading it back.
func repo(t *testing.T) (string, func(args ...string) string) {
	t.Helper()

	dir := t.TempDir()

	git := func(args ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- constants from this test
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=second-look", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=second-look", "GIT_COMMITTER_EMAIL=test@example.com")

		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}

		return strings.TrimSpace(string(out))
	}

	git("init", "--quiet", "--initial-branch", "main")
	write(t, filepath.Join(dir, "tracked.txt"), "first\n")
	git("add", "tracked.txt")
	git("commit", "--quiet", "-m", "fixture")

	return dir, git
}

func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path) // #nosec G304 -- a path this test wrote
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(raw)
}
