package tui_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

const patch = `diff --git a/internal/vcs/diff.go b/internal/vcs/diff.go
index 1111111..2222222 100644
--- a/internal/vcs/diff.go
+++ b/internal/vcs/diff.go
@@ -14,6 +14,7 @@ func Parse(r io.Reader) ([]Hunk, error) {
 	first := 1
-	lines := split(r)
+	lines, err := split(r)
+	if err != nil {
 	last := 4
diff --git a/internal/vcs/git.go b/internal/vcs/git.go
index 3333333..4444444 100644
--- a/internal/vcs/git.go
+++ b/internal/vcs/git.go
@@ -200,3 +201,3 @@ func Head() string {
 	return head
`

const parsed = "internal/vcs/diff.go"

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func comment(id, path, side string, line int, body string) artifact.Comment {
	return artifact.Comment{
		ID: id, Path: path, Side: side, Line: line, Body: body,
		Severity: "major", Status: artifact.StatusReady,
	}
}

func fixture(t *testing.T, cs ...artifact.Comment) (*tui.Model, string) {
	t.Helper()

	m, path, _ := fixtureWith(t, patch, cs...)

	return m, path
}

func fixtureWith(t *testing.T, patch string, cs ...artifact.Comment) (*tui.Model, string, *counter) {
	t.Helper()

	sub := &counter{}

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: cs,
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, sub.post)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m, path, sub
}

// press sends a keystroke and runs whatever it returned, which is what the
// program loop would do and the only way a submit reaches the submitter.
//
// A command that has not answered quickly is dropped rather than waited on. The
// search prompt's cursor schedules a blink half a second out and reschedules
// forever, and a test that waited for each one would spend its whole run
// watching a cursor the program loop runs in the background anyway.
func press(m *tui.Model, k tea.KeyPressMsg) {
	_, cmd := m.Update(k)
	if cmd == nil {
		return
	}

	const patience = 100 * time.Millisecond

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		m.Update(msg)
	case <-time.After(patience):
	}
}

// A terminal spends cells, not runes and not bytes: a CJK glyph takes two and
// an accent takes one while costing two bytes. Every frame has to land inside
// the width whatever the comment is written in.
func TestFramesFitTheirWidthInEveryScript(t *testing.T) {
	t.Parallel()

	bodies := map[string]string{
		"accented": strings.Repeat("naïve café façade résumé étude déjà vu ", 4),
		//nolint:gosmopolitan // the Han script is the test: it is what costs two cells
		"cjk":   strings.Repeat("日本語のコメントはここに書かれる。", 6),
		"emoji": strings.Repeat("shipping 🚀 this one 🎉 ", 6),
	}

	for name, body := range bodies {
		for _, width := range []int{80, 120} {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				t.Parallel()

				m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, body))
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})

				// The help overlay is measured too: it is the widest fixed text
				// the screen draws and nothing else pins it to the frame.
				press(m, tea.KeyPressMsg{Code: '?', Text: "?"})
				fitsWidth(t, "help", plain(m.Frame()), width)
				press(m, tea.KeyPressMsg{Code: '?', Text: "?"})

				frame := plain(m.Frame())
				fitsWidth(t, "review", frame, width)

				// A script that puts no spaces between words is one long word,
				// and truncating it would show the comment's first line only.
				if strings.Contains(frame, "…") {
					t.Errorf("the comment was truncated rather than wrapped:\n%s", frame)
				}
			})
		}
	}
}

// A key that silently does nothing reads as a key that is not working, and the
// end of the review is where a reader presses tab one more time.
func TestRunningOutOfCommentsSaysSo(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the only one"))

	go2(m, ']', 'c')
	go2(m, ']', 'c')

	if want := "no comment after this one"; !strings.Contains(plain(m.Frame()), want) {
		t.Errorf("want %q in the frame:\n%s", want, plain(m.Frame()))
	}

	// The next real move clears it, so the hints are not hidden by a message
	// about a key press that is already over.
	press(m, tea.KeyPressMsg{Code: 'k', Text: "k"})

	if strings.Contains(plain(m.Frame()), "no comment after") {
		t.Error("the boundary message outlived the keystroke that caused it")
	}
}

// The grammar is what makes a two-key motion cost two keys once: name it, then
// walk it with n, while the repeat key replays the last change. Triaging a
// whole review is therefore one motion followed by alternating repeats.
func TestMotionRepeatsAndDotReplaysTheChange(t *testing.T) {
	t.Parallel()

	m, path := fixture(t,
		comment("c1", parsed, artifact.SideRight, 15, "first"),
		comment("c2", parsed, artifact.SideRight, 14, "second"),
		comment("c3", "internal/vcs/git.go", artifact.SideRight, 201, "third"))

	// n before any motion, and . before any change, say so rather than guessing.
	press(m, tea.KeyPressMsg{Code: 'n', Text: "n"})

	if !strings.Contains(plain(m.Frame()), "no motion to repeat") {
		t.Error("n with nothing to repeat said nothing")
	}

	press(m, tea.KeyPressMsg{Code: '.', Text: "."})

	if !strings.Contains(plain(m.Frame()), "nothing to repeat") {
		t.Error(". with nothing to repeat said nothing")
	}

	go2(m, ']', 'c')
	first := m.CommentUnderCursor()

	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	press(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	press(m, tea.KeyPressMsg{Code: '.', Text: "."})
	press(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	press(m, tea.KeyPressMsg{Code: '.', Text: "."})

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	for i := range saved.Comments {
		if saved.Comments[i].Status != artifact.StatusSkip {
			t.Errorf("%s is %q; one motion then repeats should have skipped all three",
				saved.Comments[i].ID, saved.Comments[i].Status)
		}
	}

	// The backward prefix walks the same objects the other way, so two of them
	// come back to where the first motion landed. Comments are ordered by where
	// they anchor, not by how they were written, which is why this compares
	// against that index rather than zero.
	go2(m, '[', 'c')
	go2(m, '[', 'c')

	if got := m.CommentUnderCursor(); got != first {
		t.Errorf("[c twice from the last comment landed on %d, want %d", got, first)
	}
}

// An unfinished motion must not swallow the next keystroke, and escape has to
// get out of it, since a prefix nobody meant is the easiest key to mistype.
func TestAPendingMotionCancels(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "only"))

	go2(m, ']', 'z')

	if !strings.Contains(plain(m.Frame()), "no motion for z") {
		t.Errorf("an unknown object said nothing:\n%s", plain(m.Frame()))
	}

	press(m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Escape canceled rather than quitting or resolving, so the screen still
	// answers an ordinary key.
	press(m, tea.KeyPressMsg{Code: 'G', Text: "G"})

	if strings.Contains(plain(m.Frame()), "no motion for") {
		t.Error("escape resolved the prefix instead of canceling it")
	}
}

// Search is a motion like any other: committing a pattern is what n repeats, so
// a search and a jump to the next hunk are walked with the same key. And the
// scope is the part no other reviewer offers -- matches inside hunks nobody has
// read yet, which is the question a second pass actually asks.
func TestSearchIsAMotionAndCanBeScopedToUnread(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the only one"))

	typeSearch(m, "split")

	if got := m.CursorText(); !strings.Contains(got, "split") {
		t.Fatalf("the cursor landed on %q, which does not match", got)
	}

	first := m.CursorRow()

	// n walks to the next match without naming the pattern again.
	press(m, tea.KeyPressMsg{Code: 'n', Text: "n"})

	if m.CursorRow() == first {
		t.Error("n did not repeat the search")
	}

	if got := m.CursorText(); !strings.Contains(got, "split") {
		t.Errorf("n landed on %q, which does not match", got)
	}

	// Nothing is read, so an unread-scoped search finds the same rows. A screen
	// with nowhere to record what is read finds none, which is the honest
	// answer rather than silently searching everything.
	press(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	press(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	typeInto(m, "split")
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !strings.Contains(plain(m.Frame()), "no unread match") {
		t.Errorf("an unread search with nowhere to record read state found something:\n%s",
			plain(m.Frame()))
	}
}

// A pattern with an uppercase letter is matched exactly, which is the rule
// every editor uses and the one nobody has to be told.
func TestSearchIsCaseInsensitiveUntilItIsNot(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the only one"))

	typeSearch(m, "SPLIT")

	if !strings.Contains(plain(m.Frame()), "no match for SPLIT") {
		t.Errorf("an uppercase pattern matched lowercase text:\n%s", plain(m.Frame()))
	}

	press(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	typeSearch(m, "SPLIT(")

	if !strings.Contains(plain(m.Frame()), "no match") {
		t.Error("the second search should also find nothing")
	}
}

func typeSearch(m *tui.Model, pattern string) {
	press(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	typeInto(m, pattern)
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

func typeInto(m *tui.Model, text string) {
	for _, r := range text {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// go2 types a two-key motion: the prefix, then the object.
func go2(m *tui.Model, prefix, object rune) {
	press(m, tea.KeyPressMsg{Code: prefix, Text: string(prefix)})
	press(m, tea.KeyPressMsg{Code: object, Text: string(object)})
}

func fitsWidth(t *testing.T, what, frame string, width int) {
	t.Helper()

	for i, line := range strings.Split(frame, "\n") {
		if got := xansi.StringWidth(line); got > width {
			t.Errorf("%s line %d is %d cells wide in a %d-column frame: %q", what, i, got, width, line)
		}
	}
}

// refuser is a submitter that fails the way gh does, in one long sentence.
type refuser struct{ err error }

func (r *refuser) post(context.Context, *artifact.Review) (string, error) { return "", r.err }

// A post that fails has to be readable and has to outlive the screen. The
// footer is one line and gh's refusals are not, and the alternate screen takes
// the frame with it, so an error only shown there is an error nobody keeps.
func TestAFailedSubmitIsReadableAndReported(t *testing.T) {
	t.Parallel()

	//nolint:err113 // the text is the point: this stands in for what gh writes
	fail := errors.New("submitting: checking the pull request head: reading pull request #42: " +
		"running gh: exit status 1: gh: Resource not accessible by integration (HTTP 403)")

	rev := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Comments: []artifact.Comment{comment("c1", parsed, artifact.SideRight, 15, "check err")},
	}
	path := filepath.Join(t.TempDir(), "pr-42.toml")

	if err := artifact.Save(path, rev); err != nil {
		t.Fatal(err)
	}

	r := &refuser{err: fail}
	m := tui.New(t.Context(), rev, diff.Parse([]byte(patch)), path, r.post)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})
	press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})

	if !errors.Is(m.Failure(), fail) {
		t.Errorf("the screen kept %v, want the submit failure", m.Failure())
	}

	frame := plain(m.Frame())
	for _, want := range []string{"Resource not accessible", "(HTTP 403)"} {
		if !strings.Contains(frame, want) {
			t.Errorf("want %q in the frame, got:\n%s", want, frame)
		}
	}

	for i, line := range strings.Split(frame, "\n") {
		if len([]rune(line)) > 80 {
			t.Errorf("line %d is %d columns wide: %q", i, len([]rune(line)), line)
		}
	}
}

// Posting is asynchronous, so the keys pressed while it is in flight arrive
// before the result that sets posted. Four fast S presses must still post once.
func TestSubmittingTwiceBeforeTheFirstAnswers(t *testing.T) {
	t.Parallel()

	m, _, sub := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	var cmds []tea.Cmd

	for range 4 {
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	for _, cmd := range cmds {
		m.Update(cmd())
	}

	if sub.n != 1 {
		t.Errorf("the review posted %d times, want 1", sub.n)
	}
}

// counter is a submitter that records how many times it ran, since what the
// confirmation buys is that a keystroke on its own does not post.
type counter struct{ n int }

func (c *counter) post(context.Context, *artifact.Review) (string, error) {
	c.n++

	return "posted 3 comments", nil
}

// A comment renders under the line it anchors to, or the reader cannot tell
// which line it is about, which is the one thing the screen exists to show.
func TestCommentsRenderUnderTheirAnchor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		c     artifact.Comment
		above string
	}{
		{"added line", comment("c1", parsed, artifact.SideRight, 15, "on the add"), "lines, err := split(r)"},
		{"removed line", comment("c2", parsed, artifact.SideLeft, 15, "on the remove"), "lines := split(r)"},
		{"context line", comment("c3", parsed, artifact.SideRight, 14, "on the context"), "first := 1"},
		{"second file", comment("c4", "internal/vcs/git.go", artifact.SideRight, 201, "on the tail"), "return head"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, _ := fixture(t, tc.c)
			lines := strings.Split(plain(m.Frame()), "\n")

			at := -1

			for i, l := range lines {
				if strings.Contains(l, tc.c.Body) {
					at = i
				}
			}

			if at < 1 {
				t.Fatalf("the comment body never rendered:\n%s", strings.Join(lines, "\n"))
			}

			// Walk up off the comment block to the diff line it hangs from.
			for at > 0 && strings.Contains(lines[at], "\u2502 ") {
				at--
			}

			if !strings.Contains(lines[at], tc.above) {
				t.Errorf("the comment hangs from %q, want %q", lines[at], tc.above)
			}
		})
	}
}

// Staging refuses a comment the diff does not carry, so one reaching the screen
// means the diff moved underneath it. Hiding it would hide the problem.
func TestACommentOutsideTheDiffIsStillListed(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", "internal/gone.go", artifact.SideRight, 9, "orphaned finding"))

	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})

	got := plain(m.Frame())
	if !strings.Contains(got, "orphaned finding") || !strings.Contains(got, "not in this diff") {
		t.Errorf("an unanchored comment vanished:\n%s", got)
	}
}

func TestNoRowOverflowsTheFrame(t *testing.T) {
	t.Parallel()

	sizes := []struct{ w, h int }{{80, 24}, {100, 30}, {200, 50}}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()

			m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15,
				strings.Repeat("a very long sentence that has to wrap somewhere sensible ", 6)))
			m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})

			lines := strings.Split(plain(m.Frame()), "\n")
			if len(lines) != size.h {
				t.Errorf("rendered %d lines, want %d", len(lines), size.h)
			}

			for i, l := range lines {
				if n := len([]rune(l)); n > size.w {
					t.Errorf("line %d is %d wide, want at most %d: %q", i, n, size.w, l)
				}
			}
		})
	}
}

// Skipping is one keystroke, and the schema requires a reason, so the keystroke
// has to supply one or the review stops validating.
func TestSkippingFillsInAReason(t *testing.T) {
	t.Parallel()

	m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "on the add"))

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if saved.Comments[0].Status != artifact.StatusSkip {
		t.Errorf("status = %q, want skip", saved.Comments[0].Status)
	}
	if saved.Comments[0].SkipReason == "" {
		t.Error("a skip was saved with no reason, which will not validate")
	}
	if err := saved.Validate(); err != nil {
		t.Errorf("the saved review no longer validates: %v", err)
	}
}

func TestTooSmallAFrameSaysSo(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})

	if got := plain(m.Frame()); !strings.Contains(got, "80x10") {
		t.Errorf("render = %q, want the minimum size", got)
	}
}

// A jump to a file lands with the file's content under it. Scrolling by the
// least that reaches the heading would put it on the frame's last line, which
// is the one place the reader cannot see what the file changed.
func TestJumpingToAFileShowsItsContent(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, longPatch(t))
	go2(m, ']', 'f')
	go2(m, ']', 'f')

	lines := strings.Split(plain(m.Frame()), "\n")

	at := -1

	for i, l := range lines {
		if strings.Contains(l, "second/file.go") {
			at = i
		}
	}

	if at < 0 {
		t.Fatalf("the file never rendered:\n%s", strings.Join(lines, "\n"))
	}

	if at > 3 {
		t.Errorf("the file landed on line %d of %d, want it near the top", at, len(lines))
	}

	if !strings.Contains(strings.Join(lines[at:], "\n"), "second line 20") {
		t.Errorf("the file's own lines are off screen:\n%s", strings.Join(lines[at:], "\n"))
	}
}

// Posting is the one thing the screen cannot take back, and S sits a shift away
// from the keys that mark a comment ready.
func TestSubmitAsksFirst(t *testing.T) {
	t.Parallel()

	ready := comment("c1", parsed, artifact.SideRight, 15, "on the add")

	tests := []struct {
		name  string
		after []tea.KeyPressMsg
		posts int
		frame string
	}{
		{"asks", nil, 0, "S again to post"},
		{"confirmed", []tea.KeyPressMsg{{Code: 'S', Text: "S"}}, 1, "posted 3 comments"},
		{"canceled", []tea.KeyPressMsg{{Code: 'j', Text: "j"}}, 0, "nothing was posted"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, _, sub := fixtureWith(t, patch, ready)
			press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})

			for _, k := range tc.after {
				press(m, k)
			}

			if sub.n != tc.posts {
				t.Errorf("the review posted %d time(s), want %d", sub.n, tc.posts)
			}

			if got := plain(m.Frame()); !strings.Contains(got, tc.frame) {
				t.Errorf("the footer never said %q:\n%s", tc.frame, got)
			}
		})
	}
}

// A draft blocks the post, so the refusal moves the cursor onto the comment
// that has to be decided rather than only counting it.
func TestADraftStopsTheSubmitAndIsShown(t *testing.T) {
	t.Parallel()

	draft := comment("c2", "internal/vcs/git.go", artifact.SideRight, 201, "still thinking")
	draft.Status = artifact.StatusDraft

	m, _, sub := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 15, "on the add"), draft)
	m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})

	if sub.n != 0 {
		t.Fatalf("a draft review posted %d time(s)", sub.n)
	}

	got := plain(m.Frame())
	if !strings.Contains(got, "1 comment(s) still draft") {
		t.Errorf("the refusal never said what blocked it:\n%s", got)
	}

	if !strings.Contains(got, draft.Body) {
		t.Errorf("the cursor never reached the draft:\n%s", got)
	}
}

// longPatch is two files long enough that the second cannot be reached without
// scrolling.
func longPatch(t *testing.T) string {
	t.Helper()

	var b strings.Builder

	for _, name := range []string{"first/file.go", "second/file.go"} {
		fmt.Fprintf(&b, "diff --git a/%s b/%s\nindex 1111111..2222222 100644\n--- a/%s\n+++ b/%s\n@@ -1,40 +1,40 @@\n",
			name, name, name, name)

		for i := 1; i <= 40; i++ {
			fmt.Fprintf(&b, "+%s line %d\n", strings.TrimSuffix(name, "/file.go"), i)
		}
	}

	return b.String()
}

// Posting removes the prepared review, and that removal is what stops
// `second-look post` from publishing the same review twice. A keystroke that
// wrote the file back would undo it, so after a post the screen refuses every
// key that changes a comment.
func TestNothingIsWrittenBackAfterAPost(t *testing.T) {
	t.Parallel()

	m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "on the add"))

	press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})
	press(m, tea.KeyPressMsg{Code: 'S', Text: "S"})

	if err := os.Remove(path); err != nil {
		t.Fatalf("standing in for the removal a post does: %v", err)
	}

	for _, k := range []tea.KeyPressMsg{
		{Code: tea.KeyTab},
		{Code: 'x', Text: "x"},
		{Code: 'd', Text: "d"},
		{Code: 'r', Text: "r"},
		{Code: 'e', Text: "e"},
	} {
		press(m, k)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the prepared review came back, so posting again would post it twice")
	}

	if got := plain(m.Frame()); !strings.Contains(got, "already posted") {
		t.Errorf("the refusal is not shown:\n%s", got)
	}
}
