package tui_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/tui"
)

// gf opens the whole file in $EDITOR, which is the escape hatch + and - are
// the cheap version of. The editor is a script that echoes its arguments and
// the file and then lingers, so the test sees the +N that puts the line under
// the cursor and the file's own lines the diff never carried. It opens at
// the line a peek points at when the cursor is on the peek, and at the line of
// the whole file when that is what is open.
func TestGFOpensTheFileInAPane(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\necho \"argv $@\"\ncat \"$2\"\nsleep 30\n"
	//nolint:gosec // a file the child runs has to be executable
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)

	for _, tt := range []struct { //nolint:paralleltest // t.Setenv above
		name  string
		setup func(*testing.T, *tui.Model)
		want  string
	}{
		{"the diff", func(*testing.T, *tui.Model) {}, "argv +15"},
		{"a peek", peekAt, "argv +4"},
		{"the whole file", openedFile, "argv +4"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := peekableWith(t, &prober{jump: diag.Jump{
				Name: "split", At: []diag.Site{{Path: "io/read.go", Line: 4}},
			}}, func(context.Context, string) ([]string, error) {
				return []string{
					"package io", "", "// split reads r into fields.", "func split(r io.Reader) {}",
					"the part the diff never showed",
				}, nil
			})

			onto(t, m, "lines, err := split(r)")
			tt.setup(t, m)
			drive(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
			drive(m, tea.KeyPressMsg{Code: 'f', Text: "f"})

			if frame := paneFrame(t, m); !strings.Contains(frame, tt.want) {
				t.Errorf("the editor was not opened with %q:\n%s", tt.want, frame)
			}

			m.PaneClosed(nil)

			if frame := plain(m.Frame()); strings.Contains(frame, "argv") {
				t.Errorf("the pane stayed on screen after the child left:\n%s", frame)
			}
		})
	}
}

// paneFrame polls for the frame once the child has drawn, since it draws when
// it draws: the pane's body holds what the editor printed, which includes the
// line number it was opened at.
func paneFrame(t *testing.T, m *tui.Model) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for {
		if frame := plain(m.Frame()); strings.Contains(frame, "argv") {
			return frame
		}

		if time.Now().After(deadline) {
			t.Fatalf("the pane never drew the file:\n%s", plain(m.Frame()))
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func openedFile(t *testing.T, m *tui.Model) {
	t.Helper()

	peekAt(t, m)
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// peekAt opens a peek on the added line, which the prober answers with line 4
// of io/read.go.
func peekAt(t *testing.T, m *tui.Model) {
	t.Helper()

	press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	press(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	drive(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
}

// fileAt is a file of twenty lines whose line fifteen is the added one.
func fileAt(_ context.Context, path string) ([]string, error) {
	out := make([]string, 20)
	for i := range out {
		out[i] = "line " + strconv.Itoa(i+1) + " of " + path
	}

	out[14] = "\tlines, err := split(r)"

	return out, nil
}

// enter on a peek opens the whole file at head, and the jump list walks back
// from it to the exact row and scroll the peek was left on: ctrl-o back, then
// ctrl-i and tab forward, and esc back one. A file the diff changes marks its
// changed lines in the gutter, and one it does not says so in the header.
func TestEnterOpensTheFileAndTheJumpListWalksBack(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		site    diag.Site
		outside bool
		cursor  string
		marked  string
	}{
		{"outside the diff", diag.Site{Path: "io/read.go", Line: 12}, true, "line 12 of io/read.go", ""},
		{"inside the diff", diag.Site{Path: parsed, Line: 15}, false, "lines, err := split(r)", "15 +"},
	} {
		for _, size := range [][2]int{{80, 24}, {200, 60}} {
			t.Run(fmt.Sprintf("%s/%dx%d", tt.name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				m := peekableWith(t, &prober{jump: diag.Jump{Name: "split", At: []diag.Site{tt.site}}}, fileAt)
				resize(m, size[0], size[1])

				go2(t, m, ']', 'h')
				onto(t, m, splitLine)

				diffRow := plain(m.Frame())

				peekAt(t, m)

				peeked := plain(m.Frame())
				if peeked == diffRow {
					t.Fatalf("no peek opened:\n%s", peeked)
				}

				press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

				whole := plain(m.Frame())
				framed(t, "file", whole)
				fitsWidth(t, "the file", whole, size[0])

				if got := strings.Contains(whole, "outside the diff"); got != tt.outside {
					t.Errorf("outside the diff = %v, want %v:\n%s", got, tt.outside, whole)
				}

				if !strings.Contains(cursorText(m), tt.cursor) {
					t.Errorf("the cursor is not on %q:\n%s", tt.cursor, whole)
				}

				if !strings.Contains(whole, tt.marked) {
					t.Errorf("the changed line is not marked in the gutter:\n%s", whole)
				}

				walkBack(t, m, diffRow, peeked, whole)
			})
		}
	}
}

// walkBack walks the jump list from the file back to the diff and forward
// again, checking the frame at each stop is the one that was left there.
func walkBack(t *testing.T, m *tui.Model, diffRow, peeked, whole string) {
	t.Helper()

	for _, step := range []struct {
		key  tea.KeyPressMsg
		want string
		at   string
	}{
		{tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}, peeked, "ctrl+o to the peek"},
		{tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}, diffRow, "ctrl+o to the diff"},
		{tea.KeyPressMsg{Code: 'i', Mod: tea.ModCtrl}, peeked, "ctrl+i to the peek"},
		{tea.KeyPressMsg{Code: tea.KeyTab}, whole, "tab to the file"},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, peeked, "esc from the file"},
	} {
		press(t, m, step.key)

		if got := plain(m.Frame()); got != step.want {
			t.Fatalf("after %s the screen is not where it was:\n%s\nwant\n%s", step.at, got, step.want)
		}
	}
}

// Every motion that moves the cursor is a jump: ctrl-o returns to the row it
// left, and tab, which also walks to the next comment, keeps that job until a
// ctrl-o has left a place ahead.
func TestMotionsAreJumps(t *testing.T) {
	t.Parallel()

	m := peekable(t, &prober{})

	start := m.CursorRow()

	go2(t, m, ']', 'h')

	if m.CursorRow() == start {
		t.Fatal("]h did not move")
	}

	moved := m.CursorRow()

	press(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

	if m.CursorRow() != start {
		t.Errorf("ctrl+o left the cursor on row %d, not %d", m.CursorRow(), start)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

	if m.CursorRow() != moved {
		t.Errorf("tab after ctrl+o did not go forward: row %d, not %d", m.CursorRow(), moved)
	}

	press(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	press(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

	if frame := plain(m.Frame()); !strings.Contains(frame, "no earlier place") {
		t.Errorf("ctrl+o with nothing behind said:\n%s", frame)
	}
}
