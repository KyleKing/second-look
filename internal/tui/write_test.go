package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/tui"
)

// GitHub has no comment that is not on a line, so standing on a file's own
// heading and writing one anchors to the file's first changed line instead of
// refusing, and says so in the title rather than claiming to be about that
// line.
func TestAOnAFileHeadingAnchorsItsFirstLine(t *testing.T) {
	t.Parallel()

	m, path := fixture(t)

	toFileHead(t, m)

	pressKey(t, m, 'a')
	pressKey(t, m, 'n')

	frame := plain(m.Frame())
	if !strings.Contains(frame, ", the file") {
		t.Fatalf("the editor does not say this is about the file:\n%s", frame)
	}

	typeIn(t, m, "this file wants a different name")
	press(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	saved := reviewAt(t, path).Comments
	if len(saved) != 1 {
		t.Fatalf("comments = %+v", saved)
	}

	if c := saved[0]; c.Line == 0 {
		t.Errorf("a file comment carries no line to anchor to: %+v", c)
	}
}

// toFileHead puts the cursor on the first file heading in the diff.
func toFileHead(tb testing.TB, m *tui.Model) {
	tb.Helper()

	go2(tb, m, ']', 'f')
}

// The scrollbar says how far through the diff the cursor is but not what kind
// of row it is standing on, so landing on a file's or a hunk's own heading by
// a motion key names it beside the percentage.
func TestPositionNamesTheHeadingTheCursorLandsOn(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)

	toFileHead(t, m)

	if frame := plain(m.Frame()); !strings.Contains(frame, "file ") {
		t.Errorf("a file heading does not say so in the title:\n%s", frame)
	}

	go2(t, m, ']', 'h')

	if frame := plain(m.Frame()); !strings.Contains(frame, "hunk ") {
		t.Errorf("a hunk heading does not say so in the title:\n%s", frame)
	}
}
