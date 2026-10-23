package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

const wrapPatch = `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1,3 +1,3 @@
 func f() {
-	short
+	the quick brown fox jumps over the lazy dog and keeps on running past every fence the frame can hold
 }
`

// A line longer than the frame is the point of some diffs, and an ellipsis
// where the tail went is the one place a cut is wrong. The u chord folds it
// onto continuation lines; a second press puts the cut back.
func TestWrapFoldsALongLineRatherThanCuttingIt(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, wrapPatch)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if strings.Contains(plain(m.Frame()), "frame can hold") {
		t.Fatal("the long line's tail is drawn without wrap")
	}

	go2(t, m, 'u', 'w')

	frame := plain(m.Frame())
	if !strings.Contains(frame, "frame can hold") {
		t.Errorf("the line's tail is not drawn:\n%s", frame)
	}

	// The row still counts as one whatever it takes to draw, so a motion does
	// not have to walk its continuations.
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if strings.Contains(plain(m.Frame()), "no motion") {
		t.Error("j stalled inside a wrapped row")
	}

	go2(t, m, 'u', 'w')

	if strings.Contains(plain(m.Frame()), "frame can hold") {
		t.Error("u w did not put the cut back")
	}
}

// Wrap makes a row several lines tall where every bound the cursor keeps is
// counted in rows, so the frame has to fit the cursor's row rather than only
// reach its index.
func TestWrapKeepsTheCursorRowOnScreen(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,12 @@\n")

	for i := range 10 {
		fmt.Fprintf(&b, "+%s%d\n", strings.Repeat("x", 300), i)
	}

	m, _, _ := fixtureWith(t, b.String())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})

	go2(t, m, 'u', 'w')
	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "x9") {
		t.Fatalf("the last row is not drawn:\n%s", frame)
	}

	bar := -1
	for i, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "▌") {
			bar = i
		}
	}

	if bar < 0 {
		t.Errorf("the cursor's row is nowhere on the frame:\n%s", frame)
	}
}
