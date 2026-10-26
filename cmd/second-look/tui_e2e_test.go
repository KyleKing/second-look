package main_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
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

// size is a terminal's width and height in cells.
type size struct{ cols, rows int }

// winsize is sz as a pty reports it. A terminal's size is far below what the
// field holds.
func (sz size) winsize() *pty.Winsize {
	return &pty.Winsize{Cols: uint16(sz.cols), Rows: uint16(sz.rows)} //nolint:gosec // see above
}

// emulate is a terminal of sz. It answers the queries it is asked on a pipe
// nobody else reads, and a write that waits on that pipe would stall the
// reader, so the pipe is drained. The emulator is never closed, because its
// Close races its own reader, and the drain ends with the test binary.
func emulate(sz size) *vt.SafeEmulator {
	term := vt.NewSafeEmulator(sz.cols, sz.rows)

	go io.Copy(io.Discard, term) //nolint:errcheck // it ends when the emulator closes

	return term
}

// defaultSize is the terminal a screen test opens on unless it says otherwise.
var defaultSize = size{ptyCols, ptyRows}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// screen is a pty running the binary, with everything it has written so far.
type screen struct {
	t   *testing.T
	pty *os.File
	mu  sync.Mutex
	buf strings.Builder
	cmd *exec.Cmd
	// termMu serializes the emulator, whose String reads the screen without
	// taking the lock its Write holds.
	termMu sync.Mutex
	// term is what a terminal would be showing, fed everything the program
	// writes, so a test can read the frame the reader sees rather than the
	// stream that drew it. A resize replaces it with an emulator of the new
	// size, since the library's own resize leaves the old scroll region behind
	// and panics on the next scroll.
	term *vt.SafeEmulator
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

	return openAt(t, defaultSize, s, dir, args...)
}

// openAt is openReview on a terminal of the size named.
func openAt(t *testing.T, sz size, s *ghcassette.Session, dir string, args ...string) *screen {
	t.Helper()

	env := append(childEnv(t, s, dir), "TERM=xterm-256color")

	for len(args) > 1 && strings.Contains(args[0], "=") {
		env = append(env, args[0])
		args = args[1:]
	}

	cmd := exec.CommandContext(t.Context(), binary, args...) // #nosec G204 -- the binary TestMain built
	cmd.Dir = dir
	cmd.Env = env

	f, err := pty.StartWithSize(cmd, sz.winsize())
	if err != nil {
		t.Fatalf("starting the review screen on a pty: %v", err)
	}

	sc := &screen{t: t, pty: f, cmd: cmd, drained: make(chan struct{}), term: emulate(sz)}

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

		s.feed(chunk[:n])

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
//
// It reads a stream of writes rather than a rendered terminal, so a redraw that
// only rewrites the cells that changed leaves the tail of a line and not its
// start. Never wait on a footer or status line whose opening words may already
// be on screen; wait on text a full redraw writes, and let the pty's own input
// buffer order the keystrokes.
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

// awaitFrom returns the position right after want, so a caller chaining a
// second wait off this one gets a mark with no gap to race: one taken by a
// separate call to mark() could land after text the second wait needs to see,
// since the screen keeps writing between the two calls.
func (s *screen) awaitFrom(mark int, want string) int {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if text := s.text(); len(text) > mark {
			if i := strings.Index(text[mark:], want); i >= 0 {
				return mark + i + len(want)
			}
		}

		time.Sleep(20 * time.Millisecond)
	}

	s.t.Fatalf("waiting for %q; the screen wrote:\n%s", want, s.text())

	return 0
}

// awaitPlayed blocks until the program has made want recorded gh calls, because a
// screen fills in while its searches still run and quitting early drops the rest.
func (s *screen) awaitPlayed(session *ghcassette.Session, want int) {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(session.Played(s.t)) >= want {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	s.t.Fatalf("waiting for %d gh calls, saw %d; the screen wrote:\n%s", want, len(session.Played(s.t)), s.text())
}

// feed shows what the program wrote. A chunk drawn for the old size can land in
// the emulator of the new one after a resize, and the library panics scrolling
// a row past its end, so that chunk is dropped: the redraw that follows a
// resize paints the whole frame again.
func (s *screen) feed(chunk []byte) {
	s.termMu.Lock()
	defer s.termMu.Unlock()

	defer func() { _ = recover() }() //nolint:errcheck // see above

	_, _ = s.term.Write(chunk) //nolint:errcheck // the emulator only fails once it is closed
}

// resize changes the terminal's size the way a window does: the pty's size
// changes and the program is signaled, so it redraws for the new one.
func (s *screen) resize(sz size) {
	s.t.Helper()

	if err := pty.Setsize(s.pty, sz.winsize()); err != nil {
		s.t.Fatalf("resizing the terminal to %dx%d: %v", sz.cols, sz.rows, err)
	}

	s.termMu.Lock()
	defer s.termMu.Unlock()

	s.term = emulate(sz)
}

// reversedRows is the rows of the terminal with a reversed cell in them.
func (s *screen) reversedRows() []int {
	s.termMu.Lock()
	defer s.termMu.Unlock()

	var rows []int

	for y := range s.term.Height() {
		for x := range s.term.Width() {
			if c := s.term.CellAt(x, y); c != nil && c.Style.Attrs&uv.AttrReverse != 0 {
				rows = append(rows, y)

				break
			}
		}
	}

	return rows
}

// frame is what the terminal is showing now, one string per row.
func (s *screen) frame() []string {
	s.termMu.Lock()
	defer s.termMu.Unlock()

	return strings.Split(s.term.String(), "\n")
}

// awaitFrame blocks until the terminal shows a frame want accepts, and fails
// with that frame when it never does.
func (s *screen) awaitFrame(what string, want func(frame []string) bool) []string {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if frame := s.frame(); want(frame) {
			return frame
		}

		time.Sleep(20 * time.Millisecond)
	}

	s.t.Fatalf("waiting for %s; the terminal showed:\n%s", what, strings.Join(s.frame(), "\n"))

	return nil
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
	sc.await("3 ready · ◐ 1 draft · 1 skipped")

	// A draft blocks the submit rather than posting or vanishing, and the screen
	// jumps to it so the person rules on the one it stopped for.
	sc.press("S")
	sc.await("still draft")
	sc.press("mr")
	sc.await("is ready")

	// Sa, Sr, and Sc each name what the review is posted as, and naming it is
	// the only way to post: there is no key for "send it as whatever it says".
	sc.press("S")
	sc.press("c")
	sc.await("posted 4 comments")

	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	// The alternate screen owns the terminal until the screen exits, so the two
	// lines the post wrote are held back and land in the scrollback.
	out := sc.text()
	for _, want := range []string{
		"posted /repos/KyleKing/second-look/pulls/2/reviews",
		"removed the staged review for KyleKing/second-look#2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in the scrollback, got:\n%s", want, out)
		}
	}

	if _, err := os.Stat(artifact.Path(stored(t, dir), 2)); !os.IsNotExist(err) {
		t.Error("the prepared review outlived the submit")
	}

	s.RequireAllPlayed(t)
}

// A todo reply posts like any reply, and the extra step names who gets the fix:
// n posts it and hands it to nobody, which adds no call.
func TestReviewScreenAsksWhoGetsATodoReplyFix(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)

	s := ghcassette.Replay(t, deriveFrom(t, "post-reply", "todo-reply", func(c *ghcassette.Cassette) {
		inCheckout(c)
		restamp(c, sha)

		all := c.Interactions
		c.Interactions = slices.Concat(all[:reads], threadInteraction(t),
			[]ghcassette.Interaction{commitsInteraction(nil, nil)}, all)
	}))

	raw, err := os.ReadFile(filepath.Join("testdata", "review", "reply.toml"))
	if err != nil {
		t.Fatalf("reading the fixture review: %v", err)
	}

	todo := strings.NewReplacer(fixtureHeadSHA, sha, `status = "ready"`, `status = "todo"`).Replace(string(raw))
	write(t, artifact.Path(stored(t, dir), 2), []byte(todo))

	sc := openReview(t, s, dir, "2")
	sc.await("testdata/fixture/sample.go")
	sc.press("S")
	sc.press("c")
	sc.await("fixes go to  [l]ocal agent  [n]obody")
	sc.press("n")
	sc.await("posted 1 reply")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	s.RequireAllPlayed(t)
}

// A review reached without a get caches the threads itself, because nothing
// else will: a queue row opens straight into the screen, and a diff shown
// without the answers already on it is the second pass silently missing.
func TestReviewScreenFetchesThreadsWithoutAGet(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, withCommits(t, openCassette(t, sha)))
	seedReview(t, dir, sha)

	sc := openReview(t, s, dir, "2")
	sc.await("testdata/fixture/sample.go")

	// ]t is what puts the thread on screen. Whether it fits under the review's
	// own prose depends on how much of that there is, which is not what this
	// test is about.
	sc.press("]t")
	sc.await("unresolved")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	s.RequireAllPlayed(t)

	if _, err := os.Stat(artifact.ThreadsPath(stored(t, dir), sha)); err != nil {
		t.Fatalf("the threads were not cached: %v", err)
	}
}

// A review already staged and read once is drawn out of the artifact tree
// alone. The head check is the only call left and it runs behind the first
// frame, so a warm open waits for nothing: the cassette answers that one call
// and a fetch of the diff or the threads fails on an unrecorded one. The head
// it answers with is not the staged one, which is how the report of a head that
// moved is drawn rather than refusing the open.
func TestReviewScreenOpensFromTheCache(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, headCassette(t))
	seedReview(t, dir, sha)
	seedDiffAt(t, dir, sha)
	seedThreads(t, dir, sha)

	sc := openReview(t, s, dir, "2")
	sc.await("testdata/fixture/sample.go")
	sc.await("the head moved to")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	s.RequireAllPlayed(t)
}

// Answering a conversation already on the pull request is the second pass this
// tool exists for. The thread comes from the recording, the reply is written in
// $EDITOR, and what lands is a comment addressed to a real GitHub comment id.
func TestReviewScreenRepliesToAnOpenThread(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, withCommits(t, openOnlyCassette(t, sha)))
	seedReview(t, dir, sha)
	seedThreads(t, dir, sha)

	editor := filepath.Join(t.TempDir(), "editor")
	// The script reads a line before writing, so the reply only lands if the
	// pane forwards the keystrokes the screen's frame asks it to.
	script := "#!/bin/sh\nIFS= read -r line\nprintf '%s\\n' \"$line\" > \"$1\"\n"

	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the editor: %v", err)
	}

	sc := openReview(t, s, dir, "EDITOR="+editor, "2")
	sc.await("testdata/fixture/sample.go")

	// ]t names the thread; e opens the editor in the frame, and ctrl+e hands
	// what is in it to $EDITOR, which runs in the pane under the diff.
	sc.press("]t")
	sc.await("unresolved")
	sc.press("e")
	sc.await("ctrl+s save")
	sc.press("\x05")
	sc.await("keys go to it")
	sc.press("Answered from the review screen.")
	sc.press("\r")
	sc.await("staged, ready to post")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
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
	s := ghcassette.Replay(t, withCommits(t, openCassette(t, sha)))
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
	set, err := seen.Load(seen.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatalf("the read hunks: %v", err)
	}

	patch, err := artifact.LoadDiff(stored(t, dir), sha)
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
// shell runs on a real pty in the pane, which is why this is a pty test.
func TestReviewScreenAttachesAShellTranscript(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, withCommits(t, openCassette(t, sha)))
	seedReview(t, dir, sha)

	shell := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\necho 'total is wrong for negative entries'\n"

	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the shell: %v", err)
	}

	sc := openReview(t, s, dir, "SHELL="+shell, "2")
	sc.await("testdata/fixture/sample.go")

	// ]c rather than tab: tab lands on whatever wants a decision next, which is
	// an open thread once the screen caches them, and the note goes on a comment.
	sc.press("]c")
	sc.press("!")
	sc.await("stays local")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
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

// The pane's child owns the keyboard, which leaves no way out of a program
// that will not leave on its own. The screen keeps one key for itself, and
// ending the child has to hand the screen back.
func TestReviewScreenEndsAWedgedPane(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, withCommits(t, openCassette(t, sha)))
	seedReview(t, dir, sha)

	shell := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\nexec sleep 60\n"

	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the shell: %v", err)
	}

	sc := openReview(t, s, dir, "SHELL="+shell, "2")
	sc.await("testdata/fixture/sample.go")

	sc.press("]c")
	sc.press("!")
	sc.await("keys go to it")
	sc.press("\x1c")
	sc.await("signal: killed")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	s.RequireAllPlayed(t)
}

// The skill tells an agent not to open the review screen, and an agent that
// does has no terminal. What it gets back has to name the command to run
// instead, since Bubble Tea's own refusal reports a missing device.
func TestReviewScreenRefusesWithoutATerminal(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, withCommits(t, openCassette(t, sha)))
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
		{"ctrl-c", "\x03"},
	} {
		t.Run(quit.name, func(t *testing.T) {
			t.Parallel()

			dir, sha := scratchRepo(t, headBranch)
			s := ghcassette.Replay(t, withCommits(t, openCassette(t, sha)))
			seedReview(t, dir, sha)

			sc := openReview(t, s, dir, "2")
			sc.await("testdata/fixture/sample.go")
			sc.press(quit.keys)

			if code := sc.wait(); code != 0 {
				t.Fatalf("quitting exited %d:\n%s", code, sc.text())
			}

			if _, err := os.Stat(artifact.Path(stored(t, dir), 2)); err != nil {
				t.Error("quitting removed the prepared review")
			}

			requireTerminalGivenBack(t, sc.raw())
		})
	}
}

// reviewThenInbox is a review's reads followed by the inbox's, which is what a
// review opened from the command line costs once esc asks for the queue.
func reviewThenInbox(t *testing.T, sha string) string {
	t.Helper()

	c, err := ghcassette.Load(withCommits(t, openCassette(t, sha)))
	if err != nil {
		t.Fatalf("loading the review's reads: %v", err)
	}

	c.Interactions = append(c.Interactions, onScreen(t).Interactions...)

	path := filepath.Join(t.TempDir(), "review-then-inbox.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

// A review opened by naming it has no queue behind it, and esc asks for one: the
// inbox opens, and q from there ends the session with the terminal given back.
func TestReviewEsc_OpensTheQueueFromADirectReview(t *testing.T) {
	t.Parallel()

	for _, sz := range queueSizes {
		t.Run(fmt.Sprintf("%dx%d", sz.cols, sz.rows), func(t *testing.T) {
			t.Parallel()

			dir, sha := scratchRepo(t, headBranch)
			s := ghcassette.Replay(t, reviewThenInbox(t, sha))
			seedReview(t, dir, sha)

			sc := openAt(t, sz, s, dir, "2")
			at := sc.awaitFrom(0, "testdata/fixture/sample.go")

			sc.press("\x1b")
			sc.awaitFrom(at, "gh-sweep#102")
			sc.press("q")

			if code := sc.wait(); code != 0 {
				t.Fatalf("leaving the queue exited %d:\n%s", code, sc.text())
			}

			requireTerminalGivenBack(t, sc.raw())
		})
	}
}

// privateMode matches a DEC private mode being set or reset, which is how a
// program takes the alternate screen, hides the cursor, and asks for the mouse
// and for pasted text to be marked.
var privateMode = regexp.MustCompile(`\x1b\[\?(\d+)([hl])`)

// requireTerminalGivenBack fails unless what a program wrote leaves the
// terminal as it found it. Every quit path has to leave the alternate screen,
// or the terminal is left showing a frame nobody can scroll out of, show the
// cursor again, and turn off each input mode it turned on.
func requireTerminalGivenBack(t *testing.T, raw string) {
	t.Helper()

	const (
		altScreen = "1049"
		cursor    = "25"
	)

	last := map[string]string{}
	for _, m := range privateMode.FindAllStringSubmatch(raw, -1) {
		last[m[1]] = m[2]
	}

	if last[altScreen] != "l" {
		t.Error("the alternate screen was not restored")
	}

	if got, hid := last[cursor]; hid && got != "h" {
		t.Error("the cursor was left hidden")
	}

	for mode, state := range last {
		if mode != cursor && state == "h" {
			t.Errorf("private mode %s was turned on and left on", mode)
		}
	}
}

// A checkout holding the pull request's commits reads by them; one missing them
// says so in the plan's basis rather than fetching.
func TestReviewScreenOrdersByThePullRequestsCommits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		held   bool
		expect string
	}{
		{name: "held", held: true, expect: "by commit"},
		{name: "absent", expect: "this checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir, base := scratchRepo(t, headBranch)
			names := []string{"x/a.txt", "y/b.txt"}
			ids := commitFiles(t, dir, names...)
			head := ids[len(ids)-1]

			if !tc.held {
				ids = []string{strings.Repeat("1", len(head)), strings.Repeat("2", len(head))}
			}

			patch := gitIn(t, dir, "diff", "--no-ext-diff", "--no-color", base, head) + "\n"
			s := ghcassette.Replay(t, commitsCassette(t, head, patch, ids, names))

			sc := openReview(t, s, dir, "2")
			sc.await("y/b.txt")
			sc.press("i")
			sc.await(tc.expect)
			sc.press("qq")

			if code := sc.wait(); code != 0 {
				t.Fatalf("the screen exited %d:\n%s", code, sc.text())
			}

			s.RequireAllPlayed(t)
		})
	}
}

// commitFiles adds one commit per file, each writing that file alone, and
// answers the commits' ids oldest first.
func commitFiles(t *testing.T, dir string, names ...string) []string {
	t.Helper()

	ids := make([]string, 0, len(names))

	for _, name := range names {
		write(t, filepath.Join(dir, name), []byte(name+"\n"))
		gitIn(t, dir, "add", name)
		gitIn(t, dir, "commit", "--quiet", "-m", "add "+name)

		ids = append(ids, gitIn(t, dir, "rev-parse", "HEAD"))
	}

	return ids
}

// commitsCassette is the open recording standing on head with patch as the
// pull request's diff, followed by gh's answer for the commits listed.
func commitsCassette(t *testing.T, head, patch string, ids, names []string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "commits", func(c *ghcassette.Cassette) {
		inCheckout(c)
		restamp(c, head)

		c.Interactions[1].Stdout = patch

		c.Interactions = append(append(c.Interactions[:reads:reads], threadInteraction(t)...),
			commitsInteraction(ids, names))
	})
}
