package main_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	main "github.com/kyleking/second-look/cmd/second-look"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/local"
)

func TestLocalFlags(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		mode local.Mode
		base string
	}{
		{"the bare command is the working copy", nil, local.Worktree, ""},
		{"--branch is the branch", []string{"--branch"}, local.Branch, ""},
		{"--base implies the branch", []string{"--base", "main"}, local.Branch, "main"},
		{"both spell it twice", []string{"--branch", "--base", "main"}, local.Branch, "main"},
		{"--range is a git range", []string{"--range", "main..feat"}, local.Range, "main..feat"},
		{"-r is a jj revset", []string{"-r", "trunk()..@"}, local.Revset, "trunk()..@"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mode, base, err := main.LocalFlags(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if mode != tc.mode || base != tc.base {
				t.Errorf("%v parses to mode %d base %q", tc.args, mode, base)
			}
		})
	}

	for _, args := range [][]string{
		{"--base"},
		{"nonsense"},
		{"--base", "main", "extra"},
		{"--range"},
		{"--range", "a..b", "-r", "@"},
		{"--branch", "--range", "a..b"},
		{"--range", "a..b", "--branch"},
	} {
		if _, _, err := main.LocalFlags(args); err == nil {
			t.Errorf("%v should have been refused", args)
		}
	}
}

// A run with no terminal prints the diff itself: the same patch the screen
// would draw, writing nothing.
//
//nolint:paralleltest // the command reads "." so the test moves the process's directory
func TestLocalPipe(t *testing.T) {
	dir, _ := scratchRepo(t, "main")

	write(t, filepath.Join(dir, "a.txt"), []byte("one\n"))
	gitCommit(t, dir)
	write(t, filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"))

	t.Chdir(dir)

	var out strings.Builder
	if err := main.LocalPipe(t.Context(), nil, &out); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "+two") {
		t.Fatalf("the pipe carried %q, not the working copy's patch", out.String())
	}
	if _, err := os.Stat(artifact.LocalPath(dir)); err == nil {
		t.Error("a pipe wrote a review; printing a diff touches nothing")
	}
}

func gitCommit(t *testing.T, dir string) {
	t.Helper()

	for _, args := range [][]string{{"add", "."}, {"commit", "--quiet", "-m", "fixture"}} {
		cmd := exec.CommandContext(context.Background(), "git", args...) // #nosec G204 -- a fixture's own constants
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// The review local.toml describes is the checkout's own, so adopting pull
// request state must leave it in the working copy.
func TestLocalStaysOnAdopt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	dir, _ := scratchRepo(t, "main")

	write(t, artifact.LocalPath(dir), []byte("version = 1\nlocal = true\nrepo = \"scratch\"\n"))
	write(t, artifact.Path(dir, 2), []byte(`version = 1
host = "github.com"
owner = "KyleKing"
repo = "second-look"
number = 2
head_sha = "`+fixtureHeadSHA+`"
`))

	if err := artifact.AdoptHere(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(artifact.LocalPath(dir)); err != nil {
		t.Error("the local review was adopted into the store")
	}
}
