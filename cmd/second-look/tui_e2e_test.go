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
	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
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
	// drained closes once everything the screen wrote has been read. A process
	// that has exited may still have bytes in the pty, so a test asserting on
	// the last thing it wrote -- the escape that gives the alternate screen
	// back -- has to wait for the reader and not just for the exit.
	drained chan struct{}
}

// raw is everything written, escapes included, for an assertion about the
// terminal state rather than about the text.
func (s *screen) raw() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.String()
}

// openReview starts the screen on a pty. Anything before the first argument
// that is not a flag or a number is an environment assignment, which is how a
// test hands the screen its own $EDITOR.
func openReview(t *testing.T, s *ghcassette.Session, dir string, args ...string) *screen {
	t.Helper()

	env := append(childEnv(t, s), "TERM=xterm-256color")

	for len(args) > 1 && strings.Contains(args[0], "=") {
		env = append(env, args[0])
		args = args[1:]
	}

	cmd := exec.CommandContext(t.Context(), binary, args...) // #nosec G204 -- the binary TestMain built
	cmd.Dir = dir
	cmd.Env = env

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: ptyCols, Rows: ptyRows})
	if err != nil {
		t.Fatalf("starting the review screen on a pty: %v", err)
	}

	sc := &screen{t: t, pty: f, cmd: cmd, drained: make(chan struct{})}

	go sc.drain()

	t.Cleanup(func() { _ = f.Close() }) //nolint:errcheck // the process is already gone by then

	return sc
}

// answers is what a terminal replies to the capabilities Bubble Tea asks
// about. A bare pty answers none of them, so the program waits out a two-second
// timeout on each before it draws anything, which is most of the time these
// tests used to spend and the reason they timed out under load.
//
//nolint:gochecknoglobals // a table of constants, kept beside the loop that reads it
var answers = []struct{ query, reply string }{
	{"\x1b]11;?", "\x1b]11;rgb:1e1e/1e1e/1e1e\a"},
	{"\x1b[c", "\x1b[?62;22c"},
	{"\x1b[?2026$p", "\x1b[?2026;2$y"},
	{"\x1b[?2027$p", "\x1b[?2027;2$y"},
}

func (s *screen) drain() {
	defer close(s.drained)

	chunk := make([]byte, 4096)

	for {
		n, err := s.pty.Read(chunk)

		s.mu.Lock()
		s.buf.Write(chunk[:n])
		s.mu.Unlock()

		s.answer(string(chunk[:n]))

		if err != nil {
			return
		}
	}
}

func (s *screen) answer(written string) {
	for _, a := range answers {
		if strings.Contains(written, a.query) {
			//nolint:errcheck // the process is gone by the time this can fail
			_, _ = s.pty.WriteString(a.reply)
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
	s.awaitFrom(0, want)
}

// mark is how much has been written so far, for a test that has to tell one
// draw from the next. A screen that closes and reopens writes the same text
// twice, and await over the whole buffer would answer from the first draw and
// send the next keystroke into a program that has not started yet.
// It counts what text() answers with rather than raw bytes, since that is what
// awaitFrom slices.
func (s *screen) mark() int { return len(s.text()) }

func (s *screen) awaitFrom(mark int, want string) {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if text := s.text(); len(text) > mark && strings.Contains(text[mark:], want) {
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

	code := 0

	if err := s.cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			s.t.Fatalf("waiting for the screen to exit: %v", err)
		}

		code = exitErr.ExitCode()
	}

	// The pty answers EIO once the process is gone, which is what ends the
	// reader. A deadline rather than a bare receive, so a reader that somehow
	// does not end fails here instead of hanging the suite.
	const patience = 5 * time.Second

	select {
	case <-s.drained:
	case <-time.After(patience):
		s.t.Fatalf("the screen exited but its output was still being read:\n%s", s.text())
	}

	return code
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

// Answering a conversation already on the pull request is the second pass this
// tool exists for. The thread comes from the recording, the reply is written in
// $EDITOR, and what lands is a comment addressed to a real GitHub comment id.
func TestReviewScreenRepliesToAnOpenThread(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))
	seedReview(t, dir, sha)
	seedThreads(t, dir, sha)

	editor := filepath.Join(t.TempDir(), "editor")
	script := "#!/bin/sh\nprintf 'Answered from the review screen.\\n' > \"$1\"\n"

	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the editor: %v", err)
	}

	sc := openReview(t, s, dir, "EDITOR="+editor, "2")
	sc.await("open thread")

	sc.press("\t")
	sc.await("e reply")
	sc.press("e")
	sc.await("staged, ready to post")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	review, err := artifact.Load(filepath.Join(dir, ".second-look", "pr-2.toml"))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	var reply *artifact.Comment

	for i := range review.Comments {
		if review.Comments[i].InReplyTo != 0 {
			reply = &review.Comments[i]
		}
	}

	if reply == nil {
		t.Fatal("no reply was staged")
	}

	if reply.Body != "Answered from the review screen." {
		t.Errorf("the reply reads %q", reply.Body)
	}

	if reply.Status != artifact.StatusReady {
		t.Errorf("the reply is %q; a reply the person just typed is ruled on", reply.Status)
	}
}

// Reading a diff is the other half of reviewing one, and what makes it work is
// that the marks outlive the session. ]u walks what is left, n repeats it, and
// space marks it, so a review is finished when nothing answers ]u.
func TestReviewScreenMarksHunksRead(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))
	seedReview(t, dir, sha)

	sc := openReview(t, s, dir, "2")
	sc.await("testdata/fixture/sample.go")

	sc.press("]u")
	sc.press(" ")
	sc.await("hunk read")

	sc.press("]u")
	sc.await("no unread hunk")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	// The screen is gone, so what says the hunk was read is the file it left.
	set, err := seen.Load(seen.Path(dir, 2))
	if err != nil {
		t.Fatalf("the read hunks: %v", err)
	}

	patch, err := artifact.LoadDiff(dir, sha)
	if err != nil {
		t.Fatalf("the cached diff: %v", err)
	}

	refs := seen.Hunks(diff.Parse(patch))
	if got := set.Count(refs); got != len(refs) {
		t.Errorf("%d of %d hunks came back read", got, len(refs))
	}
}

// Attaching evidence is the flow the schema's local note exists for: run the
// code under review, come back, and what it printed is on the comment. The
// terminal has to be handed over for real, which is why this is a pty test.
func TestReviewScreenAttachesAShellTranscript(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))
	seedReview(t, dir, sha)

	shell := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\necho 'total is wrong for negative entries'\n"

	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the shell: %v", err)
	}

	sc := openReview(t, s, dir, "SHELL="+shell, "2")
	sc.await("testdata/fixture/sample.go")

	sc.press("\t")
	sc.await("r/d/x state")
	sc.press("!")
	sc.await("stays local")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	review, err := artifact.Load(filepath.Join(dir, ".second-look", "pr-2.toml"))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	var attached *artifact.Comment

	for i := range review.Comments {
		if strings.Contains(review.Comments[i].Note, "total is wrong for negative entries") {
			attached = &review.Comments[i]
		}
	}

	if attached == nil {
		t.Fatalf("no comment carries the transcript:\n%s", sc.text())
	}

	// The note is local by construction, and a transcript is the thing that
	// would hurt most to leak: it is whatever the reviewer's terminal printed.
	if strings.Contains(attached.Body, "total is wrong") {
		t.Error("the transcript reached the body, which posts")
	}
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
			if !strings.Contains(sc.raw(), "\x1b[?1049l") {
				t.Error("the alternate screen was not restored")
			}
		})
	}
}
