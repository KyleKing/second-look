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
	"github.com/kyleking/second-look/internal/structure"
	"github.com/kyleking/second-look/internal/threads"
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
func nextView(m *tui.Model) { press(m, tea.KeyPressMsg{Code: 'c', Text: "c"}) }

// The state chord is m then a letter, which is how a comment is restamped.
func state(m *tui.Model, letter rune) {
	press(m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	press(m, tea.KeyPressMsg{Code: letter, Text: string(letter)})
}

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

// TestTreeDecidesWhatCheckoutAndShellCanDo is the lazy checkout. A review opens
// whether or not a working copy is on its head, so asking for one is C, and a
// shell that would run against something else is refused by name.
func TestTreeDecidesWhatCheckoutAndShellCanDo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		tree     tui.Tree
		wants    bool
		says     string
		refusing string
	}{
		{
			name: "standing on the head", tree: tui.TreeOnHead,
			says: "already on this pull request",
		},
		{
			name: "standing on another branch", tree: tui.TreeElsewhere, wants: true,
			refusing: "C moves it onto this pull request",
		},
		{
			name: "no checkout of it here", tree: tui.TreeNone,
			says: "clone it first", refusing: "would run somewhere else",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := treeFixture(t, tc.tree)

			press(m, tea.KeyPressMsg{Code: 'C', Text: "C"})

			if got := m.WantsCheckout(); got != tc.wants {
				t.Errorf("C asked for a checkout = %v, want %v", got, tc.wants)
			}

			if tc.says != "" && !strings.Contains(plain(m.Frame()), tc.says) {
				t.Errorf("C never said %q:\n%s", tc.says, plain(m.Frame()))
			}

			if tc.refusing == "" {
				return
			}

			// A shell on the head is the working path and launches one, so only
			// the two that refuse are driven here.
			press(m, tea.KeyPressMsg{Code: '!', Text: "!"})

			if !strings.Contains(plain(m.Frame()), tc.refusing) {
				t.Errorf("! never said %q:\n%s", tc.refusing, plain(m.Frame()))
			}
		})
	}
}

// Merging is the least reversible thing the screen does, so it takes the key
// twice, refuses while the review is still staged, and refuses outright where
// there is nothing to merge with.
func TestMergeAsksTwiceAndRefusesAStagedReview(t *testing.T) {
	t.Parallel()

	t.Run("a staged review is not merged", func(t *testing.T) {
		t.Parallel()

		m, merges := mergeFixture(t, comment("c1", parsed, "RIGHT", 16, "a word"))
		pressKey(m, 'M')
		pressKey(m, 'M')

		if *merges != 0 {
			t.Errorf("%d merge(s) sent with a review still staged", *merges)
		}

		if !strings.Contains(plain(m.Frame()), "still staged") {
			t.Errorf("the refusal does not say why:\n%s", plain(m.Frame()))
		}
	})

	t.Run("one press arms and the second sends", func(t *testing.T) {
		t.Parallel()

		m, merges := mergeFixture(t)

		pressKey(m, 'M')

		if *merges != 0 {
			t.Error("the first M merged without confirming")
		}

		if !strings.Contains(plain(m.Frame()), "M again") {
			t.Errorf("the first M did not ask:\n%s", plain(m.Frame()))
		}

		pressKey(m, 'M')

		if *merges != 1 {
			t.Errorf("%d merge(s) after confirming, want 1", *merges)
		}
	})

	t.Run("any other key cancels", func(t *testing.T) {
		t.Parallel()

		m, merges := mergeFixture(t)

		pressKey(m, 'M')
		pressKey(m, 'j')
		pressKey(m, 'M')

		if *merges != 0 {
			t.Errorf("%d merge(s) after a cancel", *merges)
		}
	})
}

func pressKey(m *tui.Model, c rune) {
	press(m, tea.KeyPressMsg{Code: c, Text: string(c)})
}

// mergeFixture is a review with nothing staged unless comments are given, and a
// merger that counts rather than merging.
func mergeFixture(t *testing.T, cs ...artifact.Comment) (*tui.Model, *int) {
	t.Helper()

	merges := 0
	sub := &counter{}

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: cs,
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, sub.post,
		tui.WithMerger(func(context.Context, *artifact.Review) (string, error) {
			merges++

			return "merged", nil
		}))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m, &merges
}

func treeFixture(t *testing.T, tree tui.Tree) *tui.Model {
	t.Helper()

	sub := &counter{}
	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Comments: []artifact.Comment{comment("c1", parsed, "RIGHT", 16, "a word")},
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, sub.post, tui.WithTree(tree))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m
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

	state(m, 'x')
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

// t is the syntax-aware half of the same filter. A reworded comment changes no
// code, and comparing the two sides line by line cannot tell: the lines differ.
func TestCosmeticHunksFoldAway(t *testing.T) {
	t.Parallel()

	if !structure.Available() {
		t.Skip("ast-grep is not installed")
	}

	patch := `diff --git a/x.py b/x.py
--- a/x.py
+++ b/x.py
@@ -1,3 +1,3 @@
 def total(rows):
-    # old wording
+    # new wording
     return sum(rows)
@@ -20,2 +20,3 @@
 def f():
+    return 1
`

	m, _, _ := fixtureWith(t, patch)

	// The pass costs a subprocess per hunk side, which is more than the shared
	// helper's patience, so the command is waited on here.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if cmd == nil {
		t.Fatal("t started no structural pass")
	}

	m.Update(cmd())

	frame := plain(m.Frame())
	if strings.Contains(frame, "new wording") {
		t.Errorf("the comment-only hunk is still shown:\n%s", frame)
	}

	if !strings.Contains(frame, "1 hunk hidden: no code changed") {
		t.Errorf("nothing says a hunk was hidden:\n%s", frame)
	}

	if !strings.Contains(frame, "return 1") {
		t.Errorf("t hid a hunk that changes something:\n%s", frame)
	}

	pressKey(m, 't')

	if !strings.Contains(plain(m.Frame()), "new wording") {
		t.Error("t did not bring the hunk back")
	}
}

// A reformat buried among real changes is the thing that makes a diff feel
// long. Hiding it has to say how much was hidden and take it out of the read
// count too, or the count could never reach its own total.
func TestWhitespaceHunksFoldAway(t *testing.T) {
	t.Parallel()

	patch := `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1,3 +1,3 @@
 package x
-  a := 1
+	a := 1
@@ -20,2 +20,3 @@
 func f() {}
+// a real change
`

	m, _, _ := fixtureWith(t, patch)

	if !strings.Contains(plain(m.Frame()), "a := 1") {
		t.Fatal("the whitespace hunk is not shown to begin with")
	}

	press(m, tea.KeyPressMsg{Code: 'w', Text: "w"})

	frame := plain(m.Frame())
	if strings.Contains(frame, "a := 1") {
		t.Errorf("the whitespace hunk is still shown:\n%s", frame)
	}

	if !strings.Contains(frame, "1 hunk hidden: whitespace only") {
		t.Errorf("nothing says a hunk was hidden:\n%s", frame)
	}

	if !strings.Contains(frame, "a real change") {
		t.Error("folding hid a hunk that changes something")
	}

	press(m, tea.KeyPressMsg{Code: 'w', Text: "w"})

	if !strings.Contains(plain(m.Frame()), "a := 1") {
		t.Error("w did not bring the hunk back")
	}
}

// Reading a change is reading one package at a time, and a flat list of paths
// makes the reader do that grouping in their head on every scroll. The heading
// carries the counts so a directory can be taken or left as a unit, and ]d
// walks them.
func TestFilesAreGroupedByDirectory(t *testing.T) {
	t.Parallel()

	patch := `diff --git a/internal/tui/view.go b/internal/tui/view.go
--- a/internal/tui/view.go
+++ b/internal/tui/view.go
@@ -1,2 +1,3 @@
 package tui
+// one
diff --git a/docs/guide.md b/docs/guide.md
--- a/docs/guide.md
+++ b/docs/guide.md
@@ -1,2 +1,3 @@
 # guide
+more
diff --git a/internal/tui/model.go b/internal/tui/model.go
--- a/internal/tui/model.go
+++ b/internal/tui/model.go
@@ -1,2 +1,3 @@
 package tui
+// two
@@ -20,2 +21,3 @@
 func f() {}
+// three
`

	m, _, _ := fixtureWith(t, patch)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	frame := plain(m.Frame())
	for _, want := range []string{"internal/tui  2 files · 3 hunks", "docs  1 file · 1 hunk"} {
		if !strings.Contains(frame, want) {
			t.Errorf("want %q in the frame:\n%s", want, frame)
		}
	}

	// A directory the diff interleaves is still shown as one block, so the
	// reader never has to hold two places in one package at once.
	lines := strings.Split(frame, "\n")
	at := func(want string) int {
		for i, l := range lines {
			if strings.Contains(l, want) {
				return i
			}
		}

		return -1
	}

	a, b, docs := at("internal/tui/view.go"), at("internal/tui/model.go"), at("docs/guide.md")
	if docs > a && docs < b {
		t.Errorf("docs/guide.md was rendered between two internal/tui files:\n%s", frame)
	}

	go2(m, ']', 'd')
	first := m.CursorText()
	press(m, tea.KeyPressMsg{Code: 'n', Text: "n"})

	if m.CursorText() == first {
		t.Error("n did not repeat the directory motion")
	}
}

// The comment view is the review without the diff. What makes it worth having
// is that it is the same rows, so every motion and action still works, and that
// coming back lands on the comment you were reading rather than at the top.
func TestTheCommentViewKeepsYourPlace(t *testing.T) {
	t.Parallel()

	skipped := comment("c3", parsed, artifact.SideRight, 201, "held back")
	skipped.Status = artifact.StatusSkip
	skipped.SkipReason = "unverified"

	skipped.Path = "internal/vcs/git.go"

	m, path := fixture(t,
		comment("c1", parsed, artifact.SideRight, 15, "the split can fail"),
		skipped)

	go2(m, ']', 'c')
	was := m.CommentUnderCursor()

	// c walks three views, so the comments are two presses from the diff and
	// the diff is one press back.
	nextView(m)
	nextView(m)

	frame := plain(m.Frame())
	if !strings.Contains(frame, "1 ready · 0 draft · 0 skipped") {
		t.Errorf("the file heading does not carry the counts:\n%s", frame)
	}

	if !strings.Contains(frame, "0 ready · 0 draft · 1 skipped") {
		t.Errorf("the skipped comment's file is missing its count:\n%s", frame)
	}

	// A finding considered and declined is worth recording and not worth
	// re-reading, so it is counted rather than listed.
	if strings.Contains(frame, "held back") {
		t.Error("a skipped comment was listed rather than counted")
	}

	if !strings.Contains(frame, "the split can fail") {
		t.Errorf("the comment that will post is missing:\n%s", frame)
	}

	// Actions work here, because these are the same rows.
	state(m, 'd')
	nextView(m)

	if got := m.CommentUnderCursor(); got != was {
		t.Errorf("coming back landed on comment %d, want %d", got, was)
	}

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if saved.Comments[0].Status != artifact.StatusDraft {
		t.Errorf("d in the comment view left c1 %q", saved.Comments[0].Status)
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

// A finding worth saying now goes out on its own, and what proves it worked is
// that the comment leaves the prepared review: a copy left staged would go out
// a second time with the rest.
func TestPostingOneCommentTakesItOffTheReview(t *testing.T) {
	t.Parallel()

	draft := comment("c2", parsed, artifact.SideRight, 14, "not ruled on")
	draft.Status = artifact.StatusDraft

	m, path, _ := fixtureWith(t, patch,
		comment("c1", parsed, artifact.SideRight, 15, "the split can fail"), draft)

	var sent []string

	m.SetSender(func(_ context.Context, r *artifact.Review, id string) (string, error) {
		sent = append(sent, id)
		r.Remove(id)

		if err := artifact.Save(path, r); err != nil {
			return "", fmt.Errorf("saving: %w", err)
		}

		return "posted " + id, nil
	})

	// A draft has not been ruled on, so naming it directly still does nothing.
	go2(m, ']', 'c')
	state(m, 'd')
	press(m, tea.KeyPressMsg{Code: 'P', Text: "P"})

	if len(sent) != 0 {
		t.Fatalf("a draft was posted on its own: %v", sent)
	}

	state(m, 'r')
	press(m, tea.KeyPressMsg{Code: 'P', Text: "P"})

	if len(sent) != 1 {
		t.Fatalf("posted %v, want exactly one comment", sent)
	}

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	for i := range saved.Comments {
		if saved.Comments[i].ID == sent[0] {
			t.Error("the posted comment is still staged and would go out twice")
		}
	}

	if len(saved.Comments) != 1 {
		t.Errorf("%d comments are left, want the one that did not post", len(saved.Comments))
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

			// Walk up off the comment block, its rail and the blank row that
			// opens it, to the diff line it hangs from.
			for at > 0 && (strings.TrimSpace(lines[at]) == "" ||
				strings.Contains(lines[at], "\u2503 ") || strings.Contains(lines[at], "\u2502 ")) {
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

	go2(m, ']', 'c')

	// The three state keys sit behind m, and a bare press says so rather than
	// doing nothing, because the hand that learned them keeps reaching.
	press(m, tea.KeyPressMsg{Code: 'x', Text: "x"})

	if got := plain(m.Frame()); !strings.Contains(got, "m first") {
		t.Errorf("a bare x said nothing:\n%s", got)
	}

	if got := m.CommentStatus(0); got != artifact.StatusReady {
		t.Errorf("a bare x restamped the comment to %q", got)
	}

	state(m, 'x')

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
	if !strings.Contains(got, "1 comment still draft") {
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

	go2(m, ']', 'c')

	for _, letter := range []rune{'x', 'd', 'r'} {
		state(m, letter)
	}

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the prepared review came back, so posting again would post it twice")
	}

	if got := plain(m.Frame()); !strings.Contains(got, "already posted") {
		t.Errorf("the refusal is not shown:\n%s", got)
	}
}

// type writes into whatever prompt or editor owns the keyboard.
func typeIn(m *tui.Model, text string) {
	for _, r := range text {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Editing in the frame is what makes fixing a word cheap. It has to leave the
// line under review on screen, since a comment edited away from what it is
// about is the reason $EDITOR was the wrong shape for this.
func TestEditingHappensInTheFrame(t *testing.T) {
	t.Parallel()

	m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	go2(m, ']', 'c')
	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "editing c1") || !strings.Contains(frame, "ctrl+s save") {
		t.Fatalf("the editor is not in the frame:\n%s", frame)
	}

	if !strings.Contains(frame, "lines, err := split(r)") {
		t.Errorf("the line being commented on left the frame:\n%s", frame)
	}

	typeIn(m, ", it can fail")
	press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := saved.Comments[0].Body; got != "check err, it can fail" {
		t.Errorf("body = %q", got)
	}

	// The review's own body had nowhere to be written from: its rows carried no
	// comment, so tab walked past them and e said there was no comment here.
	press(m, tea.KeyPressMsg{Code: 'g', Text: "g"})

	if got := m.CursorText(); !strings.Contains(got, "no body, no note") {
		t.Fatalf("the screen does not open on the review's own prose: %q", got)
	}

	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	typeIn(m, "Read it twice.")
	press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	if saved = reviewAt(t, path); saved.Body != "Read it twice." {
		t.Errorf("review body = %q", saved.Body)
	}

	// The one row is two the moment either is written, so the note it now
	// carries is reachable in its own right.
	press(m, tea.KeyPressMsg{Code: tea.KeyTab})

	if got := m.CursorText(); !strings.Contains(got, "REVIEW NOTE") {
		t.Errorf("tab off the body landed on %q, want the review note", got)
	}
}

// Answering an open thread is the second pass's whole job, and the only cover
// it had was a pty test, so a motion that stopped reaching a thread would only
// have shown up as a ten-second timeout.
func TestReplyingToAnOpenThread(t *testing.T) {
	t.Parallel()

	open := threads.Thread{
		Path: parsed, Side: artifact.SideRight, Line: 15,
		Notes: []threads.Note{{ID: 77, Author: "KyleKing", Body: "does this handle nil?"}},
	}

	_, path, _ := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 14, "check err"))
	m2 := tui.New(t.Context(), reviewAt(t, path), diff.Parse([]byte(patch)), path,
		func(context.Context, *artifact.Review) (string, error) { return "", nil },
		tui.WithThreads([]threads.Thread{open}))
	m2.Init()
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	go2(m2, ']', 't')

	if got := m2.CursorText(); !strings.Contains(got, "open thread") {
		t.Fatalf("]t landed on %q", got)
	}

	if got := plain(m2.Frame()); !strings.Contains(got, "r[e]ply") {
		t.Fatalf("the footer does not offer a reply:\n%s", got)
	}

	press(m2, tea.KeyPressMsg{Code: 'e', Text: "e"})
	typeIn(m2, "yes, os.ReadFile answers a nil-safe error")
	press(m2, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	for i := range saved.Comments {
		if saved.Comments[i].InReplyTo == 77 {
			return
		}
	}

	t.Errorf("no reply was staged: %+v", saved.Comments)
}

func reviewAt(t *testing.T, path string) *artifact.Review {
	t.Helper()

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// z acts on what the cursor is standing on, so the same two keys give an
// outline of a long review, one file at a time or all of it.
func TestZFoldsAFileAHunkANoteAndTheWholeReview(t *testing.T) {
	t.Parallel()

	long := comment("c1", parsed, artifact.SideRight, 15, "check err")
	long.Note = "ran the tests\nthey pass\nagainst the fixture\nand the real thing"

	m, _ := fixture(t, long)

	// A note carries the evidence for a comment rather than the comment, so it
	// starts folded once it runs long enough to bury what it supports.
	go2(m, ']', 'c')

	if got := plain(m.Frame()); !strings.Contains(got, "NOTE  4 lines · za to read") {
		t.Fatalf("the note is not folded:\n%s", got)
	}

	go2(m, 'z', 'a')

	if got := plain(m.Frame()); !strings.Contains(got, "against the fixture") {
		t.Fatalf("za did not open the note:\n%s", got)
	}

	go2(m, 'z', 'a')

	if got := plain(m.Frame()); strings.Contains(got, "against the fixture") {
		t.Fatalf("za did not close it again:\n%s", got)
	}

	press(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	go2(m, ']', 'f')
	go2(m, 'z', 'a')

	frame := plain(m.Frame())
	if !strings.Contains(frame, "1 hunk folded") || strings.Contains(frame, "lines, err := split(r)") {
		t.Fatalf("za on the file name did not fold it:\n%s", frame)
	}

	go2(m, 'z', 'a')
	go2(m, ']', 'h')
	go2(m, 'z', 'a')

	frame = plain(m.Frame())
	if !strings.Contains(frame, "folded · za to open") || strings.Contains(frame, "lines, err := split(r)") {
		t.Fatalf("za on the hunk heading did not fold it:\n%s", frame)
	}

	go2(m, 'z', 'M')

	frame = plain(m.Frame())
	if !strings.Contains(frame, "internal/vcs/git.go  1 hunk folded") {
		t.Fatalf("zM did not fold every file:\n%s", frame)
	}

	go2(m, 'z', 'R')

	if frame = plain(m.Frame()); !strings.Contains(frame, "lines, err := split(r)") {
		t.Errorf("zR did not open it again:\n%s", frame)
	}
}

// The code view is the file as it reads after the change, which is the question
// a review turns on and the one a +/- pair leaves the reader to work out.
func TestTheCodeViewShowsTheFileThatResults(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the split can fail"))

	nextView(m)

	frame := plain(m.Frame())

	if strings.Contains(frame, "lines := split(r)") {
		t.Errorf("a removed line is still drawn:\n%s", frame)
	}

	if !strings.Contains(frame, "1 line removed") {
		t.Errorf("what came out is not accounted for:\n%s", frame)
	}

	if !strings.Contains(frame, "lines, err := split(r)") {
		t.Errorf("the line that results is missing:\n%s", frame)
	}

	// A comment stands as one row until it is asked for, so four on one hunk
	// do not bury the code they are about.
	if !strings.Contains(frame, "▸ ● MAJOR  ready  the split can fail") {
		t.Fatalf("the comment is not folded to a marker:\n%s", frame)
	}

	go2(m, ']', 'c')
	go2(m, 'z', 'a')

	if got := plain(m.Frame()); strings.Contains(got, "▸ ● MAJOR") {
		t.Errorf("za did not open the comment:\n%s", got)
	}
}

// A peek is for looking at what is above or below without giving up where you
// were, so the next motion comes back to the cursor rather than to the frame.
func TestPeekScrollsWithoutMovingTheCursor(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	go2(m, ']', 'h')
	was := m.CursorRow()

	for range 5 {
		press(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	}

	if got := m.CursorRow(); got != was {
		t.Fatalf("the peek moved the cursor to row %d, want %d", got, was)
	}

	press(m, tea.KeyPressMsg{Code: 'j', Text: "j"})

	if got := m.CursorRow(); got != was+1 {
		t.Errorf("j after a peek landed on row %d, want %d", got, was+1)
	}

	if got := plain(m.Frame()); !strings.Contains(got, "\n▌") {
		t.Errorf("the frame did not come back to the cursor:\n%s", got)
	}
}
