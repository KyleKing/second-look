// Package main_test drives the built second-look binary against recorded gh
// interactions. The seam is PATH rather than a Go interface, so what these
// tests exercise is the same code path a person runs, down to the subprocess.
package main_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/ghcassette"
)

// fixtureHeadSHA is the head of the recording-target pull request, KyleKing/second-look#2.
// The branch is never pushed to again, so the anchors stay valid.
const fixtureHeadSHA = "6bc1218809a6faf83bc266c7a10b6b096f814a74"

//nolint:gochecknoglobals // the binary under test, built once by TestMain
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "second-look-e2e")
	if err != nil {
		panic(err)
	}

	binary = filepath.Join(dir, "second-look")

	//nolint:noctx,gosec // a build of this package, not a request, and every argument is a constant
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}

	code := m.Run()

	ghcassette.RemoveStub()
	_ = os.RemoveAll(dir) //nolint:errcheck // a leftover temp directory is not worth failing a run

	os.Exit(code)
}

// cassettePath is absolute because the stub inherits the working directory of
// whatever called gh, which is the temporary workspace rather than this package.
func cassettePath(t *testing.T, name string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("testdata", "cassettes", name+".golden"))
	if err != nil {
		t.Fatalf("resolving the cassette path: %v", err)
	}

	return path
}

// workspace is a directory holding a prepared review and nothing else, which is
// all `post` reads: the anchor guard fetches the live diff rather than the
// cached one.
func workspace(t *testing.T, fixture string) string {
	t.Helper()

	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, ".second-look"), 0o750); err != nil {
		t.Fatalf("creating the workspace: %v", err)
	}

	// #nosec G304,G703 -- a fixture in this package
	raw, err := os.ReadFile(filepath.Join("testdata", "review", fixture))
	if err != nil {
		t.Fatalf("reading the fixture review: %v", err)
	}

	// #nosec G703 -- a path under the test's own temporary directory
	if err := os.WriteFile(filepath.Join(dir, ".second-look", "pr-2.toml"), raw, 0o600); err != nil {
		t.Fatalf("writing the fixture review: %v", err)
	}

	return dir
}

// derive builds a cassette from the one real recording and writes it where the
// test can reach it. The cases that never happened -- a head that moved, a run
// that stops before it posts -- are edits to the recording rather than three
// checked-in copies of the same diff.
func derive(t *testing.T, name string, fn func(*ghcassette.Cassette)) string {
	t.Helper()

	return deriveFrom(t, "post-review", name, fn)
}

func deriveFrom(t *testing.T, from, name string, fn func(*ghcassette.Cassette)) string {
	t.Helper()

	c := load(t, from)
	fn(c)

	path := filepath.Join(t.TempDir(), name+".golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

func load(t *testing.T, name string) *ghcassette.Cassette {
	t.Helper()

	c, err := ghcassette.Load(cassettePath(t, name))
	if err != nil {
		t.Fatalf("loading the %s recording: %v", name, err)
	}

	return c
}

// seedDiff writes the cached diff a staging command reads, taking it from the
// recording so the bytes are the ones GitHub sent rather than a second copy.
func seedDiff(t *testing.T, dir string) {
	t.Helper()

	c := load(t, "post-review")

	patch, err := c.Response("pr", "diff", "2")
	if err != nil {
		t.Fatalf("the cassette has no recorded diff: %v", err)
	}

	path := filepath.Join(dir, ".second-look", "diff", fixtureHeadSHA+".patch")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating the diff cache: %v", err)
	}

	if err := os.WriteFile(path, []byte(patch), 0o600); err != nil {
		t.Fatalf("writing the cached diff: %v", err)
	}
}

type result struct {
	stdout string
	stderr string
	code   int
}

func runCLI(t *testing.T, s *ghcassette.Session, dir string, args ...string) result {
	t.Helper()

	return runCLIStdin(t, s, dir, "", args...)
}

func runCLIStdin(t *testing.T, s *ghcassette.Session, dir, stdin string, args ...string) result {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binary, args...) // #nosec G204 -- the binary TestMain built
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Dir = dir
	cmd.Env = append(s.Env(t), "GH_REPO=KyleKing/second-look")

	var out, errOut strings.Builder

	cmd.Stdout = &out
	cmd.Stderr = &errOut

	res := result{}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running second-look %v: %v", args, err)
		}

		res.code = exitErr.ExitCode()
	}

	res.stdout, res.stderr = out.String(), errOut.String()

	return res
}
