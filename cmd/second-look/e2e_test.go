// Package main_test drives the built second-look binary against recorded gh
// interactions. The seam is PATH rather than a Go interface, so what these
// tests exercise is the same code path a person runs, down to the subprocess.
package main_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
)

// fixtureHeadSHA is the head of the recording-target pull request, KyleKing/second-look#2.
// The branch is never pushed to again, so the anchors stay valid.
const fixtureHeadSHA = "6bc1218809a6faf83bc266c7a10b6b096f814a74"

// headBranch is the pull request's head ref, which the recording names.
const headBranch = "fixture/review-target"

// fixtureRepo is the recording target, which the recorded reads name because
// they were made from a directory that is not a checkout of it.
const fixtureRepo = "KyleKing/second-look"

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
	coverDir = os.Getenv("COVERDIR_SUBPROCESS")

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

// workspace is a directory that is not a checkout of anything, with a prepared
// review in the store behind it. It is all `post` reads: the anchor guard
// fetches the live diff rather than the cached one, and a bare number resolves
// because the store holds exactly one review answering to it.
func workspace(t *testing.T, fixture string) string {
	t.Helper()

	dir := t.TempDir()

	// #nosec G304,G703 -- a fixture in this package
	raw, err := os.ReadFile(filepath.Join("testdata", "review", fixture))
	if err != nil {
		t.Fatalf("reading the fixture review: %v", err)
	}

	write(t, artifact.Path(stored(t, dir), 2), raw)

	return dir
}

// write puts a file where a test wants it, creating what is above it.
func write(t *testing.T, path string, body []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, body, 0o600); err != nil { // #nosec G703 -- the test's own directory
		t.Fatalf("writing %s: %v", path, err)
	}
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
	seedDiffAt(t, dir, fixtureHeadSHA)
}

// seedDiffAt caches the recorded diff against another head, which is what a
// scratch repository's own commit is.
func seedDiffAt(t *testing.T, dir, sha string) {
	t.Helper()

	c := load(t, "post-review")

	patch, err := c.Response("pr", "diff", "2", "--repo", fixtureRepo)
	if err != nil {
		t.Fatalf("the cassette has no recorded diff: %v", err)
	}

	write(t, artifact.DiffPath(stored(t, dir), sha), []byte(patch))
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

// runCLIEnv runs with extra environment, which is how a test points state that
// lives outside the checkout somewhere it can inspect.
func runCLIEnv(t *testing.T, s *ghcassette.Session, dir string, env []string, args ...string) result {
	t.Helper()

	return runCLIStdinEnv(t, s, dir, "", env, args...)
}

func runCLIStdin(t *testing.T, s *ghcassette.Session, dir, stdin string, args ...string) result {
	t.Helper()

	return runCLIStdinEnv(t, s, dir, stdin, nil, args...)
}

func runCLIStdinEnv(
	t *testing.T, s *ghcassette.Session, dir, stdin string, env []string, args ...string,
) result {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binary, args...) // #nosec G204 -- the binary TestMain built
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Dir = dir
	cmd.Env = append(childEnv(t, s, dir), env...)

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
// repository its recording names, where to record what it ran, and a home of
// its own.
//
// The home matters as much as the cassette. The queue's read marks and the
// reviews staged with no checkout both live under the user config directory, so
// a child inheriting a real one would read the state of whoever is running the
// suite and write into it. A test that wants to inspect that state appends its
// own HOME, which wins.
func childEnv(t *testing.T, s *ghcassette.Session, dir string) []string {
	t.Helper()

	home := testHome(t, dir)

	env := append(s.Env(t),
		"GH_REPO="+fixtureRepo,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
	)

	if coverDir != "" {
		env = append(env, "GOCOVERDIR="+coverDir)
	}

	return env
}

// testHome is the state directory every run inside one test shares, so a review
// staged by one command is read by the next. It sits beside the working
// directory rather than under it: a store inside a checkout makes the tree
// dirty, and the checkout guard would offer to stash it.
func testHome(t *testing.T, dir string) string {
	t.Helper()

	home := filepath.Join(filepath.Dir(dir), "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatalf("creating the state home: %v", err)
	}

	return home
}

// stored is where the fixture repository's state lands for a run in dir.
func stored(t *testing.T, dir string) string {
	t.Helper()

	return storeIn(t, testHome(t, dir))
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

	body := strings.ReplaceAll(string(raw), fixtureHeadSHA, sha)
	write(t, artifact.Path(stored(t, dir), 2), []byte(body))
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

	if err := artifact.SaveThreads(stored(t, dir), sha, open); err != nil {
		t.Fatalf("caching the threads: %v", err)
	}
}

// reviewCassette is the recording re-stamped onto the scratch head, with the
// two reads repeated. Opening the screen and guarding the submit each make
// them, and GitHub answers the same request the same way.
func reviewCassette(t *testing.T, sha string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "review-screen", func(c *ghcassette.Cassette) {
		inCheckout(c)
		restamp(c, sha)

		all := c.Interactions
		c.Interactions = append(append(all[:reads:reads], threadInteraction(t)...), all...)
	})
}

// openCassette is what a first read of a pull request costs: the two reads plus
// the GraphQL query for the open threads, whose recording lives with the code
// that makes it, in internal/threads. Splicing it in beats a second copy of the
// same answer under this package's testdata.
//
// `get` and a review screen reached without one make the same three calls,
// because the screen caches the threads itself rather than showing none.
func openCassette(t *testing.T, sha string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "open", func(c *ghcassette.Cassette) {
		inCheckout(c)
		restamp(c, sha)
		c.Interactions = append(c.Interactions[:reads:reads], threadInteraction(t)...)
	})
}

// addressed re-points a recorded interaction at another pull request, so one
// recording answers for whatever repository a test's queue happens to name.
func addressed(in ghcassette.Interaction, repo string, number int) ghcassette.Interaction {
	owner, name, _ := strings.Cut(repo, "/")
	at := strconv.Itoa(number)

	args := append([]string{}, in.Args...)
	for i, arg := range args {
		switch arg {
		case "2", "number=2":
			args[i] = strings.Replace(arg, "2", at, 1)
		case fixtureRepo:
			args[i] = repo
		case "owner=KyleKing":
			args[i] = "owner=" + owner
		case "repo=second-look":
			args[i] = "repo=" + name
		}
	}

	in.Args = args
	in.Stdout = strings.ReplaceAll(in.Stdout, `"number":2`, `"number":`+at)

	return in
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
		inCheckout(c)
		restamp(c, sha)
		c.Interactions = c.Interactions[:reads]
	})
}

// headCassette answers the head check and nothing else, so a screen that
// fetched anything at all fails on an unrecorded call. It is left on the
// recorded head rather than re-stamped onto the scratch one, which is what a
// pull request pushed to since the review was staged looks like.
func headCassette(t *testing.T) string {
	t.Helper()

	return deriveFrom(t, "post-review", "head-only", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = c.Interactions[:1]
	})
}

// reads is how many gh calls reading a pull request costs: the pull request
// itself, then its diff.
const reads = 2

// inCheckout drops the --repo the recording carries. It was recorded from a
// directory holding nothing but a prepared review, where the repository has to
// be named; a test standing in a checkout of it leaves the repository to gh,
// which is what the binary does there.
func inCheckout(c *ghcassette.Cassette) {
	for i := range c.Interactions {
		args := c.Interactions[i].Args

		for j := 0; j+1 < len(args); j++ {
			if args[j] == "--repo" {
				c.Interactions[i].Args = append(append([]string{}, args[:j]...), args[j+2:]...)

				break
			}
		}
	}
}

func restamp(c *ghcassette.Cassette, sha string) {
	for i := range c.Interactions {
		c.Interactions[i].Stdout = strings.ReplaceAll(c.Interactions[i].Stdout, fixtureHeadSHA, sha)
	}
}
