package tui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// stepHunk is one hunk of a file whose lines all carry name, so every line in
// the diff reads differently and a line can be found again by its text.
func stepHunk(name string, old, cur int) string {
	return fmt.Sprintf("@@ -%d,7 +%d,7 @@\n %[3]s one\n %[3]s two\n %[3]s three\n-%[3]s old\n+%[3]s new\n"+
		" %[3]s four\n %[3]s five\n %[3]s six\n", old, cur, name)
}

func stepFile(path string, hunks ...string) string {
	return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n" +
		strings.Join(hunks, "")
}

// aheadOfB is a file of four hunks drawn above b.go, so the line being read
// has rows above it to keep its height on the frame by.
func aheadOfB() string {
	return stepFile("a.go", stepHunk("P", 10, 10), stepHunk("Q", 40, 40), stepHunk("R", 70, 70),
		stepHunk("S", 100, 100))
}

// headAtThree is the diff at the head: a.go above, hunks A, B, and C in b.go,
// and c.go below, so there is as much to read above C as below it even once A
// and B are hidden.
func headAtThree() string {
	return aheadOfB() +
		stepFile("b.go", stepHunk("A", 10, 10), stepHunk("B", 40, 40), stepHunk("C", 70, 70)) +
		stepFile("c.go", stepHunk("D", 10, 10), stepHunk("E", 40, 40), stepHunk("F", 70, 70))
}

// frameRow is how far down the frame the cursor is drawn.
func frameRow(m *tui.Model) int { return m.CursorRow() - m.ViewOffset() }

// pushedAbove is the head after a push that added lines to b.go above hunk C,
// so every line of C carries a number five higher and reads the same. The same
// push rewrote hunk B as B2, so B's changed line is gone from the diff.
func pushedAbove() string {
	return aheadOfB() +
		stepFile("b.go", stepHunk("A", 10, 10), stepHunk("B2", 40, 42), stepHunk("C", 70, 75)) +
		stepFile("c.go", stepHunk("D", 10, 10), stepHunk("E", 40, 40), stepHunk("F", 70, 70))
}

func restageFixture(t *testing.T, h int, cs ...artifact.Comment) (*tui.Model, string) {
	t.Helper()

	m, path, _ := fixtureWith(t, headAtThree(), cs...)

	fresh := reviewAt(t, path)
	fresh.HeadSHA = "9f2c4d1"

	m.SetRestage(func(context.Context) (*tui.Restaged, error) {
		return &tui.Restaged{Review: fresh, Diff: diff.Parse([]byte(pushedAbove())), HeadSHA: fresh.HeadSHA}, nil
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: h})

	return m, path
}

func restageNow(t *testing.T, m *tui.Model) {
	t.Helper()

	m.SawHead("9f2c4d1")
	press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
}

// A push taken in mid-read renumbers the line being read. The cursor finds it
// again by what it says and draws it at the height it was at, rather than
// sending the reader back to the top of a diff they were halfway through.
func TestReload_KeepsCursorRowOnScreen(t *testing.T) {
	t.Parallel()

	for _, h := range []int{24, 30, 50} {
		t.Run(fmt.Sprintf("%d rows", h), func(t *testing.T) {
			t.Parallel()

			m, _ := restageFixture(t, h)
			onto(t, m, "C new")

			row := frameRow(m)

			restageNow(t, m)

			frame := plain(m.Frame())
			if !strings.Contains(frame, "restaged against 9f2c4d1") {
				t.Fatalf("the restage never landed:\n%s", frame)
			}

			if got, at := m.CursorText(), m.CursorAnchor(); !strings.Contains(got, "C new") || at != 78 {
				t.Errorf("the cursor is on %q at line %d rather than C's changed line at 78:\n%s", got, at, frame)
			}

			if got := frameRow(m); got != row {
				t.Errorf("the restage moved the line from row %d of the frame to row %d:\n%s", row, got, frame)
			}
		})
	}
}

// A line the push removed has nothing to be found by. The cursor lands on the
// nearest line of the same file and the status says the line went, so the move
// is not mistaken for the cursor wandering.
func TestReload_LandsOnTheNearestHunkWhenThePushDroppedTheLine(t *testing.T) {
	t.Parallel()

	m, _ := restageFixture(t, 30)
	onto(t, m, "B new")

	restageNow(t, m)

	frame := plain(m.Frame())
	if !strings.Contains(frame, "b.go:43 is not drawn now; on the nearest hunk") {
		t.Errorf("the restage did not say the line went:\n%s", frame)
	}

	if got := m.CursorFile(); got != "b.go" {
		t.Errorf("the cursor left the file for %q:\n%s", got, frame)
	}

	if got := m.CursorText(); !strings.Contains(got, "B2") {
		t.Errorf("the cursor is on %q rather than the hunk that replaced B:\n%s", got, frame)
	}
}

// A reply that posts comes off the staged file, and the watcher reloads it.
// Every comment after it moves up an index, so a cursor kept by index lands on
// the wrong comment, and one kept by nothing jumps. The comment being read
// keeps the cursor and its height on the frame.
func TestReload_KeepsCursorOnItsCommentWhenOneAboveItPosts(t *testing.T) {
	t.Parallel()

	for _, h := range []int{24, 40} {
		t.Run(fmt.Sprintf("%d rows", h), func(t *testing.T) {
			t.Parallel()

			m, path := restageFixture(t, h,
				comment("first", "b.go", artifact.SideRight, 43, "the first finding"),
				comment("second", "b.go", artifact.SideRight, 73, "the second finding"),
			)
			onto(t, m, "the second finding")

			row := frameRow(m)

			r := reviewAt(t, path)
			r.Comments = r.Comments[1:]

			if err := artifact.Save(path, r); err != nil {
				t.Fatal(err)
			}

			tick(t, m, path)

			frame := plain(m.Frame())
			if strings.Contains(frame, "the first finding") {
				t.Fatalf("the posted comment was not reloaded away:\n%s", frame)
			}

			if got := m.CursorText(); !strings.Contains(got, "the second finding") {
				t.Errorf("the cursor is on %q rather than the comment it was reading:\n%s", got, frame)
			}

			if got := frameRow(m); got != row {
				t.Errorf("the reload moved the comment from row %d of the frame to row %d:\n%s", row, got, frame)
			}
		})
	}
}
