package main_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/kyleking/second-look/internal/ghcassette"
)

// The review screen renders only on a terminal, and a pipe is not one, so these
// drive the built binary through a pty. What they check is the part a model
// test cannot see: that a real terminal gets a frame, that the alternate screen
// is given back on exit, and that the post summary reaches the scrollback
// afterwards rather than being drawn over the frame.

const (
	ptyCols = 100
	ptyRows = 30
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// screen is a pty running the binary, with everything it has written so far.
type screen struct {
	t   *testing.T
	pty *os.File
	mu  sync.Mutex
	buf strings.Builder
	cmd *exec.Cmd
}

func openReview(t *testing.T, s *ghcassette.Session, dir string, args ...string) *screen {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binary, args...) // #nosec G204 -- the binary TestMain built
	cmd.Dir = dir
	cmd.Env = append(childEnv(t, s), "TERM=xterm-256color")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: ptyCols, Rows: ptyRows})
	if err != nil {
		t.Fatalf("starting the review screen on a pty: %v", err)
	}

	sc := &screen{t: t, pty: f, cmd: cmd}

	go sc.drain()

	t.Cleanup(func() { _ = f.Close() }) //nolint:errcheck // the process is already gone by then

	return sc
}

func (s *screen) drain() {
	chunk := make([]byte, 4096)

	for {
		n, err := s.pty.Read(chunk)

		s.mu.Lock()
		s.buf.Write(chunk[:n])
		s.mu.Unlock()

		if err != nil {
			return
		}
	}
}

func (s *screen) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return plain(s.buf.String())
}

// await blocks until the screen has written want, so a test never races the
// render. The failure prints everything written, which is what makes a broken
// frame readable.
func (s *screen) await(want string) {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.text(), want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	s.t.Fatalf("waiting for %q; the screen wrote:\n%s", want, s.text())
}

func (s *screen) press(keys string) {
	s.t.Helper()

	if _, err := s.pty.WriteString(keys); err != nil {
		s.t.Fatalf("pressing %q: %v", keys, err)
	}
}

func (s *screen) wait() int {
	s.t.Helper()

	if err := s.cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			s.t.Fatalf("waiting for the screen to exit: %v", err)
		}

		return exitErr.ExitCode()
	}

	return 0
}

// TestReviewScreenSubmits is the whole premise in one run: open the review on a
// terminal, rule on the comment the agent left undecided, confirm, and post.
func TestReviewScreenSubmits(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, reviewCassette(t, sha))
	seedReview(t, dir, sha)

	sc := openReview(t, s, dir, "2")
	sc.await("testdata/fixture/sample.go")
	sc.await("3 ready · 1 draft · 1 skipped")

	// A draft blocks the submit rather than posting or vanishing, and the screen
	// jumps to it so the person rules on the one it stopped for.
	sc.press("S")
	sc.await("still draft")
	sc.press("r")
	sc.await("is ready")

	sc.press("S")
	sc.await("S again to post")
	sc.press("S")
	sc.await("posted to KyleKing/second-look #2")

	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	// The alternate screen owns the terminal until the screen exits, so the two
	// lines the post wrote are held back and land in the scrollback.
	out := sc.text()
	for _, want := range []string{
		"posted /repos/KyleKing/second-look/pulls/2/reviews",
		"removed .second-look/pr-2.toml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in the scrollback, got:\n%s", want, out)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, ".second-look", "pr-2.toml")); !os.IsNotExist(err) {
		t.Error("the prepared review outlived the submit")
	}

	s.RequireAllPlayed(t)
}

// The skill tells an agent not to open the review screen, and an agent that
// does has no terminal. What it gets back has to name the command to run
// instead, since Bubble Tea's own refusal reports a missing device.
func TestReviewScreenRefusesWithoutATerminal(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))
	seedReview(t, dir, sha)

	res := runCLI(t, s, dir, "2")
	if res.code == 0 {
		t.Errorf("the screen exited 0 with no terminal:\n%s", res.stdout)
	}

	if !strings.Contains(res.stderr, "second-look show <pr>") {
		t.Errorf("the refusal does not say what to run instead:\n%s", res.stderr)
	}
}

// TestReviewScreenQuitsWithoutPosting covers the other way out. Quitting is not
// a submit, so nothing may be sent and the prepared review has to survive.
func TestReviewScreenQuitsWithoutPosting(t *testing.T) {
	t.Parallel()

	for _, quit := range []struct {
		name string
		keys string
	}{
		{"q", "q"},
		{"esc", "\x1b"},
		{"ctrl-c", "\x03"},
	} {
		t.Run(quit.name, func(t *testing.T) {
			t.Parallel()

			dir, sha := scratchRepo(t, headBranch)
			s := ghcassette.Replay(t, openOnlyCassette(t, sha))
			seedReview(t, dir, sha)

			sc := openReview(t, s, dir, "2")
			sc.await("testdata/fixture/sample.go")
			sc.press(quit.keys)

			if code := sc.wait(); code != 0 {
				t.Fatalf("quitting exited %d:\n%s", code, sc.text())
			}

			if _, err := os.Stat(filepath.Join(dir, ".second-look", "pr-2.toml")); err != nil {
				t.Error("quitting removed the prepared review")
			}

			// Every quit path has to give the alternate screen back, or the
			// terminal is left showing a frame nobody can scroll out of.
			if !strings.Contains(sc.buf.String(), "\x1b[?1049l") {
				t.Error("the alternate screen was not restored")
			}
		})
	}
}
