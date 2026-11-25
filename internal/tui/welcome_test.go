package tui_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/coach"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// freshLedger is a ledger on its first session, saved under the test's own
// directory, which is what a first launch opens.
func freshLedger(t *testing.T) (*coach.Ledger, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coach.json")

	l, err := coach.LedgerAt(path)
	if err != nil {
		t.Fatal(err)
	}

	return l, path
}

func welcomeReview(t *testing.T, l *coach.Ledger, width, height int) *tui.Model {
	t.Helper()

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}, patch, tui.WithLedger(l))
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return m
}

// The first launch of the review screen opens on the welcome card, at both
// sizes a terminal is opened at, and the card joins its edges.
func TestWelcome_ReviewFrames(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			l, _ := freshLedger(t)
			m := welcomeReview(t, l, size[0], size[1])

			frame := m.Frame()
			for _, want := range []string{"Welcome to second-look", "]h", "next hunk", "]u", "submit the review"} {
				if !strings.Contains(frame, want) {
					t.Errorf("the card lacks %q:\n%s", want, frame)
				}
			}

			goldenFrame(t, frame)
		})
	}
}

// The help key takes the card down and opens the legend, esc only takes it
// down, the dismissal is saved at once, and a second launch is not greeted.
func TestWelcome_AnyKeyDismissesAndActsOnce(t *testing.T) {
	t.Parallel()

	l, path := freshLedger(t)
	m := welcomeReview(t, l, 100, 30)

	press(t, m, key('?'))

	if strings.Contains(m.Frame(), "Welcome to second-look") {
		t.Fatalf("? left the card up:\n%s", m.Frame())
	}

	if !strings.Contains(m.Frame(), "a key explains itself") {
		t.Errorf("? dismissed the card but did not open the legend:\n%s", m.Frame())
	}

	again, err := coach.LedgerAt(path)
	if err != nil {
		t.Fatal(err)
	}

	if coach.Due(again) || !again.Welcomed() {
		t.Error("the dismissal was not saved, so the next launch greets again")
	}

	if strings.Contains(welcomeReview(t, again, 100, 30).Frame(), "Welcome to second-look") {
		t.Error("a second launch opened on the card")
	}
}

func TestWelcome_EscOnlyDismisses(t *testing.T) {
	t.Parallel()

	l, _ := freshLedger(t)
	m := welcomeReview(t, l, 100, 30)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	frame := m.Frame()
	if strings.Contains(frame, "Welcome to second-look") {
		t.Fatalf("esc left the card up:\n%s", frame)
	}

	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("esc on the card also left the review")
		}
	}
}

// :welcome opens the card again after it was dismissed.
func TestWelcome_PaletteReopens(t *testing.T) {
	t.Parallel()

	l, _ := freshLedger(t)
	m := welcomeReview(t, l, 100, 30)
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	for _, k := range ":welcome" {
		press(t, m, key(k))
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !strings.Contains(m.Frame(), "Welcome to second-look") {
		t.Errorf(":welcome did not reopen the card:\n%s", m.Frame())
	}
}

// Below 60x16 of body the card would cover the screen it introduces, so the
// footer says it in a line instead.
func TestWelcome_SmallTerminalIsAFooterLine(t *testing.T) {
	t.Parallel()

	l, _ := freshLedger(t)
	m := welcomeReview(t, l, 80, 14)

	frame := m.Frame()
	if strings.Contains(frame, "Welcome to second-look") {
		t.Errorf("the card drew in a terminal too small for it:\n%s", frame)
	}

	if !strings.Contains(frame, "◇ welcome") {
		t.Errorf("the footer does not carry the welcome line:\n%s", frame)
	}
}

// The queue opens on the card when it is the first screen launched.
func TestWelcome_QueueFrames(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			led, _ := freshLedger(t)
			l := list(t, nil).WithLedger(led)
			l.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

			frame := l.ListFrame()
			if !strings.Contains(frame, "Welcome to second-look") {
				t.Fatalf("the queue's first launch did not open on the card:\n%s", frame)
			}

			goldenFrame(t, frame)

			l.Update(key('j'))

			if got := l.ListFrame(); strings.Contains(got, "Welcome to second-look") {
				t.Errorf("j left the card up:\n%s", got)
			}
		})
	}
}

// The queue's legend and command palette are the shared palette, pinned at both sizes: the strip
// at 80x24, the drawer with its explainer at 100x30, a gated key saying why.
func TestQueuePalette_Frames(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, mode := range []struct {
			name string
			keys []tea.KeyPressMsg
		}{
			{"browse", []tea.KeyPressMsg{key('?')}},
			{"run", []tea.KeyPressMsg{key(':'), key('m'), key('a'), key('r'), key('k')}},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode.name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				l := list(t, nil)
				l.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range mode.keys {
					l.Update(k)
				}

				goldenFrame(t, l.ListFrame())
			})
		}
	}
}

// A palette run of a legend row presses its key: : then "mark read" then
// enter marks the row under the cursor, the same path the key takes.
func TestQueuePalette_RunPressesTheKey(t *testing.T) {
	t.Parallel()

	var got []tui.Action

	l := list(t, func(a tui.Action, _ *tui.Row) (string, bool, error) {
		got = append(got, a)

		return "", false, nil
	})

	for _, k := range ":mark read" {
		l.Update(key(k))
	}

	_, cmd := l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					l.Update(c())
				}
			}

			break
		}

		_, cmd = l.Update(msg)
	}

	if len(got) != 1 || got[0] != tui.ActMark {
		t.Errorf("the palette ran %v, want one mark", got)
	}
}

// TestWelcome_LocalReviewHasNothingToSubmit is the card second-look local opened with: it taught
// "S submit the review" on a review that has nowhere to post. A local review's card names the
// notes it keeps instead, at both sizes a terminal opens at.
func TestWelcome_LocalReviewHasNothingToSubmit(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			l, _ := freshLedger(t)
			m, _, _ := modelFor(t, &artifact.Review{
				Version: artifact.SchemaVersion, Repo: "jj-diff", Local: true,
				HeadSHA: "a1b2c3d", Event: artifact.EventComment,
			}, patch, tui.WithLedger(l))
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

			frame := plain(m.Frame())
			if !strings.Contains(frame, "Welcome to second-look") || !strings.Contains(frame, "your own change") {
				t.Fatalf("a local review's first launch did not open on its card:\n%s", frame)
			}

			if strings.Contains(frame, "submit the review") || strings.Contains(frame, "pull request") {
				t.Errorf("a local review's card teaches the pull request's submit:\n%s", frame)
			}
		})
	}
}

// TestWelcome_DismissingKeyDoesNotAct is the first keystroke of a first launch:
// with the card up, a (add a comment) and S (submit) only close the card, at
// both sizes a terminal opens at, so nothing is staged or posted behind it.
func TestWelcome_DismissingKeyDoesNotAct(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		for _, k := range []rune{'a', 'S'} {
			t.Run(fmt.Sprintf("%c_%dx%d", k, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				l, _ := freshLedger(t)
				m := welcomeReview(t, l, size[0], size[1])
				press(t, m, key(k))

				lt, _ := freshLedger(t)
				twin := welcomeReview(t, lt, size[0], size[1])
				press(t, twin, tea.KeyPressMsg{Code: tea.KeyEscape})

				frame := plain(m.Frame())
				if strings.Contains(frame, "Welcome to second-look") {
					t.Fatalf("%c did not close the card:\n%s", k, frame)
				}

				if want := plain(twin.Frame()); frame != want {
					t.Errorf("%c acted behind the card; esc leaves:\n%s\n%c left:\n%s", k, want, k, frame)
				}
			})
		}
	}
}
