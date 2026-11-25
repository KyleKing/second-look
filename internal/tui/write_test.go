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

// Inside the open composer vim's keys are the field's, not the screen's: x in
// normal mode deletes a character rather than stamping the comment skip, u
// undoes the text rather than anything the review did, and the dot and r edit
// the text. The field never takes ctrl+c, so it still leaves the screen.
func TestComposer_VimKeysReachTheField(t *testing.T) {
	t.Parallel()

	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 40}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			t.Parallel()

			m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))
			m.Update(size)

			go2(t, m, ']', 'c')
			press(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})
			press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

			if frame := plain(m.Frame()); !strings.Contains(frame, "NORMAL") {
				t.Fatalf("esc did not step the field to normal mode:\n%s", frame)
			}

			editsInNormalMode(t, m)

			_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			if m.Editing() || cmd == nil || !tui.Leaves(cmd()) {
				t.Errorf("ctrl+c in the field did not leave the screen")
			}

			if got := reviewAt(t, path).Comments[0]; got.Body != "check err" || got.Status != artifact.StatusReady {
				t.Errorf("leaving wrote the comment: %+v", got)
			}
		})
	}
}

// editsInNormalMode types normal-mode commands into an open field holding
// "check err" and holds the text after each, and the comment's stamp, to what
// vim would leave.
func editsInNormalMode(t *testing.T, m *tui.Model) {
	t.Helper()

	for _, step := range []struct {
		keys, want string
	}{
		{"x", "check er"},
		{"u", "check err"},
		{"0x", "heck err"},
		{".", "eck err"},
		{"u", "heck err"},
		{"rZ", "Zeck err"},
		{"wD", "Zeck "},
		{"hvd", "Zec "},
	} {
		for _, k := range step.keys {
			press(t, m, tea.KeyPressMsg{Code: k, Text: string(k)})
		}

		if !m.Editing() {
			t.Fatalf("%q left the editor", step.keys)
		}

		if got := m.EditorText(); got != step.want {
			t.Fatalf("after %q the field holds %q, want %q", step.keys, got, step.want)
		}

		if got := m.CommentStatus(0); got != artifact.StatusReady {
			t.Fatalf("%q reached the screen: the comment is stamped %q", step.keys, got)
		}
	}
}

// A paste lands in the open composer whole, where it once reached nothing: the
// screen took the paste only for a pane or the agent's prompt.
func TestComposer_TakesAPaste(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	go2(t, m, ']', 'c')
	press(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	m.Update(tea.PasteMsg{Content: ", see\nthe caller"})

	if got := m.EditorText(); got != "check err, see\nthe caller" {
		t.Errorf("the paste left %q", got)
	}
}

// With the vim layer off the composer is a plain field: x types, and one esc
// leaves with what was typed kept, as the contract had it before the layer.
func TestComposer_WithoutVimEscLeavesAtOnce(t *testing.T) {
	t.Parallel()

	m, path, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Comments: []artifact.Comment{comment("c1", parsed, artifact.SideRight, 15, "check err")},
	}, patch, tui.WithVim(false))

	go2(t, m, ']', 'c')
	press(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "⏎ save · ctrl+j new line · esc cancel") ||
		strings.Contains(frame, "INSERT") {
		t.Fatalf("the plain field does not name its contract:\n%s", frame)
	}

	typeIn(t, m, "x")
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.Editing() {
		t.Fatal("one esc left the plain field open")
	}

	if got := reviewAt(t, path).Comments[0].Body; got != "check err" {
		t.Errorf("esc wrote the comment: %q", got)
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

				if frame := plain(m.Frame()); !strings.Contains(frame, "⏎ save · ctrl+j new line · esc normal") {
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
