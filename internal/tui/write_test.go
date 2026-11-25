package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
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

	pressKey(t, m, 'i')
	pressKey(t, m, 'n')

	frame := plain(m.Frame())
	if !strings.Contains(frame, ", the file") {
		t.Fatalf("the editor does not say this is about the file:\n%s", frame)
	}

	typeIn(t, m, "this file wants a different name")
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

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

// The composer keeps the family's prompt contract: enter saves, ctrl+j and
// alt+enter start a new line, and esc leaves without saving. The save key
// before it, ctrl+s, still saves.
func TestComposer_EnterSavesAndCtrlJBreaksTheLine(t *testing.T) {
	t.Parallel()

	ctrlJ := tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	altEnter := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	ctrlS := tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}

	for _, tc := range []struct {
		name    string
		newline tea.KeyPressMsg
		save    tea.KeyPressMsg
	}{
		{"ctrl+j then enter", ctrlJ, enter},
		{"alt+enter then enter", altEnter, enter},
		{"ctrl+j then ctrl+s", ctrlJ, ctrlS},
	} {
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 40}} {
			t.Run(fmt.Sprintf("%s/%dx%d", tc.name, size.Width, size.Height), func(t *testing.T) {
				t.Parallel()

				m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))
				m.Update(size)

				go2(t, m, ']', 'c')
				press(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})

				if frame := plain(m.Frame()); !strings.Contains(frame, "enter save · ctrl+j new line · esc cancel") {
					t.Fatalf("the editor does not name the contract:\n%s", frame)
				}

				press(t, m, tc.newline)
				typeIn(t, m, "it can fail")
				press(t, m, tc.save)

				if m.Editing() {
					t.Fatalf("%s left the editor open", tc.save.String())
				}

				if got := reviewAt(t, path).Comments[0].Body; got != "check err\nit can fail" {
					t.Errorf("body = %q, want the two lines", got)
				}
			})
		}
	}
}
