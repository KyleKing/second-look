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

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/ghcassette"
	"github.com/kyleking/second-look/internal/threads"
)

// fixtureHeadSHA is the head of the recording-target pull request, KyleKing/second-look#2.
// The branch is never pushed to again, so the anchors stay valid.
const fixtureHeadSHA = "6bc1218809a6faf83bc266c7a10b6b096f814a74"

// headBranch is the pull request's head ref, which the recording names.
const headBranch = "fixture/review-target"

//nolint:gochecknoglobals // the binary under test, built once by TestMain
var (
	binary   string
	coverDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "second-look-e2e")
	if err != nil {
		panic(err)
	}

	binary = filepath.Join(dir, "second-look")

	// Passed through explicitly rather than inherited: `go test -test.gocoverdir`
	// overwrites GOCOVERDIR in the test process, so a child that read it would
	// write into the unit run's directory and be dropped.
	coverDir = os.Getenv("SECOND_LOOK_COVERDIR")

	// -cover makes the subprocess record what it ran, which is the only way
	// these tests count: go test instruments the test binary, and everything
	// they exercise happens in a child process. The binary writes nothing
	// unless GOCOVERDIR is set, and it reaches the child through the
	// environment the cassette session passes on.
	//nolint:noctx,gosec // a build of this package, not a request, and every argument is a constant
	out, err := exec.Command("go", "build", "-cover",
		"-coverpkg=github.com/kyleking/second-look/...", "-o", binary, ".").CombinedOutput()
	if err != nil {
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
	cmd.Env = childEnv(t, s)

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

// childEnv is what the binary under test runs with: the cassette's gh, the
// repository its recording names, and where to record what it ran.
func childEnv(t *testing.T, s *ghcassette.Session) []string {
	t.Helper()

	env := append(s.Env(t), "GH_REPO=KyleKing/second-look")
	if coverDir != "" {
		env = append(env, "GOCOVERDIR="+coverDir)
	}

	return env
}

// scratchRepo is a git repository with one commit on branch, and a remote that
// names the fixture repository at an address nothing answers. The review screen
// reads the diff from the recording rather than from the working tree, so what
// the commit contains does not matter; that HEAD is a real commit and the
// remote parses does. The unreachable host is the point: gh is replayed from a
// cassette, git is not, so a path that reaches for the network fails here
// rather than passing on someone's laptop and hanging in CI.
func scratchRepo(t *testing.T, branch string) (string, string) {
	t.Helper()

	dir := t.TempDir()

	git := func(args ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- constants from this function
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

	git("init", "--quiet", "--initial-branch", branch)
	git("commit", "--quiet", "--allow-empty", "-m", "fixture")
	git("remote", "add", "origin", "https://127.0.0.1:1/KyleKing/second-look.git")

	return dir, git("rev-parse", "HEAD")
}

// seedReview writes a fixture review re-stamped onto the scratch repository's
// head, since the screen refuses a review staged against a different commit.
func seedReview(t *testing.T, dir, sha string) {
	t.Helper()

	// #nosec G304,G703 -- a fixture in this package
	raw, err := os.ReadFile(filepath.Join("testdata", "review", "staged.toml"))
	if err != nil {
		t.Fatalf("reading the fixture review: %v", err)
	}

	path := filepath.Join(dir, ".second-look", "pr-2.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating the workspace: %v", err)
	}

	body := strings.ReplaceAll(string(raw), fixtureHeadSHA, sha)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil { // #nosec G703 -- the test's own directory
		t.Fatalf("writing the fixture review: %v", err)
	}
}

// seedThreads caches the recorded conversations onto the scratch head, which is
// what `second-look get` leaves behind for the screen to read.
func seedThreads(t *testing.T, dir, sha string) {
	t.Helper()

	c := ghcassette.Cassette{Interactions: threadInteraction(t)}

	out, err := c.Response(c.Interactions[0].Args...)
	if err != nil {
		t.Fatalf("reading the recorded thread query: %v", err)
	}

	open, err := threads.Decode([]byte(out))
	if err != nil {
		t.Fatalf("reading the recorded thread query: %v", err)
	}

	if err := artifact.SaveThreads(dir, sha, open); err != nil {
		t.Fatalf("caching the threads: %v", err)
	}
}

// reviewCassette is the recording re-stamped onto the scratch head, with the
// two reads repeated. Opening the screen and guarding the submit each make
// them, and GitHub answers the same request the same way.
func reviewCassette(t *testing.T, sha string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "review-screen", func(c *ghcassette.Cassette) {
		restamp(c, sha)
		c.Interactions = append(c.Interactions[:reads:reads], c.Interactions...)
	})
}

// getCassette is what `second-look get` costs: the two reads plus the GraphQL
// query for the open threads, whose recording lives with the code that makes
// it, in internal/threads. Splicing it in beats a second copy of the same
// answer under this package's testdata.
func getCassette(t *testing.T, sha string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "get", func(c *ghcassette.Cassette) {
		restamp(c, sha)
		c.Interactions = append(c.Interactions[:reads:reads], threadInteraction(t)...)
	})
}

func threadInteraction(t *testing.T) []ghcassette.Interaction {
	t.Helper()

	path := filepath.Join("..", "..", "internal", "threads", "testdata", "cassettes", "threads.golden")

	c, err := ghcassette.Load(path)
	if err != nil {
		t.Fatalf("reading the recorded thread query: %v", err)
	}

	return c.Interactions
}

// openOnlyCassette is what opening the screen costs and nothing more, so a run
// that posts anything fails on an unrecorded call.
func openOnlyCassette(t *testing.T, sha string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "open-only", func(c *ghcassette.Cassette) {
		restamp(c, sha)
		c.Interactions = c.Interactions[:reads]
	})
}

// reads is how many gh calls reading a pull request costs: the pull request
// itself, then its diff.
const reads = 2

func restamp(c *ghcassette.Cassette, sha string) {
	for i := range c.Interactions {
		c.Interactions[i].Stdout = strings.ReplaceAll(c.Interactions[i].Stdout, fixtureHeadSHA, sha)
	}
}
