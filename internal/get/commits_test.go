package get_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/get"
)

// answerGH puts a gh on PATH that answers every call with body.
func answerGH(t *testing.T, body string) {
	t.Helper()

	bin := t.TempDir()

	answer := filepath.Join(bin, "answer")
	if err := os.WriteFile(answer, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // the stub gh on PATH has to be executable
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\ncat \"$GH_ANSWER\"\n"), 0o750); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GH_ANSWER", answer)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- a fixture's own constants
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}

	return strings.TrimSpace(string(out))
}

// A pull request's commits are mapped against the checkout when it holds them
// all, and otherwise the plan is told why it has none.
//
//nolint:paralleltest // the stub gh is put on PATH for the whole process
func TestCommitsMapsAPullRequestOnlyWhereTheCheckoutHoldsIt(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "-b", "main")
	git(t, dir, "commit", "--quiet", "--allow-empty", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")

	ids := make([]string, 0, 2)

	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		git(t, dir, "add", name)
		git(t, dir, "commit", "--quiet", "-m", "add "+name)
		ids = append(ids, git(t, dir, "rev-parse", "HEAD"))
	}

	d := diff.Parse([]byte(git(t, dir, "diff", "--no-ext-diff", "--no-color", base, "HEAD")))
	answer := `{"commits":[{"oid":%q,"messageHeadline":"add a.txt"},{"oid":%q,"messageHeadline":"add b.txt"}]}`

	answerGH(t, fmt.Sprintf(answer, ids[0], ids[1]))

	got, why, err := get.Commits(t.Context(), get.Target{Number: 7, Work: dir}, ids[1], d)
	if err != nil || why != "" {
		t.Fatalf("mapping held commits: %v %q", err, why)
	}

	if len(got) != 2 || len(got[0].Hunks) != 1 || got[0].Hunks[0].Path != "a.txt" || got[1].Hunks[0].Path != "b.txt" {
		t.Errorf("the commits came back as %+v", got)
	}

	answerGH(t, fmt.Sprintf(answer, ids[0], strings.Repeat("e", 40)))

	if got, why, err = get.Commits(t.Context(), get.Target{Number: 7, Work: dir}, ids[1], d); err != nil ||
		got != nil || why != "1 of the pull request's 2 commits are not in this checkout" {
		t.Errorf("a commit the checkout lacks answered %v, %q, %v", got, why, err)
	}

	if _, why, err = get.Commits(t.Context(), get.Target{Number: 7}, ids[1], d); err != nil || why == "" {
		t.Errorf("a review with no checkout answered %q, %v", why, err)
	}
}

// A pull request listing no commits says so rather than counting one missing.
//
//nolint:paralleltest // the stub gh is put on PATH for the whole process
func TestCommitsSaysAPullRequestListsNone(t *testing.T) {
	answerGH(t, `{"commits":[]}`)

	_, why, err := get.Commits(t.Context(), get.Target{Number: 7, Work: t.TempDir()}, "", &diff.Diff{})
	if err != nil || why != "the pull request lists no commits" {
		t.Errorf("a pull request listing no commits answered %q, %v", why, err)
	}
}
