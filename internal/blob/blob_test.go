package blob_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/blob"
)

// The checkout answers first, which is what makes growing a hunk free and
// offline. `git show` reads the commit rather than the working tree, so a tree
// left on another branch still answers for the commit under review.
func TestReadsFromTheCheckoutAtTheCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	git := func(args ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- constants
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")

		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}

		return strings.TrimSpace(string(out))
	}

	git("init", "--quiet")

	if err := os.WriteFile(filepath.Join(dir, "read.go"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	git("add", "read.go")
	git("commit", "--quiet", "-m", "one")

	sha := git("rev-parse", "HEAD")

	// The working tree moves on and the reader still answers for the commit.
	if err := os.WriteFile(filepath.Join(dir, "read.go"), []byte("rewritten\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := blob.Reader{Work: dir, SHA: sha}.Read(t.Context(), "read.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 || got[0] != "one" || got[2] != "three" {
		t.Errorf("read %q, want the file as it was at the commit", got)
	}

	// A search reads the same commit, and the working tree where none is named.
	for _, tc := range []struct {
		sha, text string
		want      []blob.Hit
	}{
		{sha, "two", []blob.Hit{{Path: "read.go", Line: 2}}},
		{sha, "rewritten", nil},
		{"", "rewritten", []blob.Hit{{Path: "read.go", Line: 1}}},
	} {
		hits, err := blob.Reader{Work: dir, SHA: tc.sha}.Grep(t.Context(), tc.text)
		if err != nil || !slices.Equal(hits, tc.want) {
			t.Errorf("searching %q at %q found %v, %v; want %v", tc.text, tc.sha, hits, err, tc.want)
		}
	}
}

// With no checkout holding the commit and no repository to ask about, the
// reader says so rather than reaching for something.
func TestReadWithNoSource(t *testing.T) {
	t.Parallel()

	if _, err := (blob.Reader{}).Read(t.Context(), "read.go"); err == nil {
		t.Error("a reader with nothing to read from answered")
	}

	if _, err := (blob.Reader{SHA: "abc"}).Read(t.Context(), "read.go"); err == nil {
		t.Error("a reader with no checkout and no repository answered")
	}

	if _, err := (blob.Reader{SHA: "abc"}).Grep(t.Context(), "two"); err == nil {
		t.Error("a search with no checkout answered")
	}
}
