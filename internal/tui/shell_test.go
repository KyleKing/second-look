package tui_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// errNoSuchReview stands in for an open that could not be read, which belongs
// in the queue's footer rather than taking the screen down.
var errNoSuchReview = errors.New("gh said no")

// pump runs what a command answered back into the shell, the way the program
// loop does, until nothing is left, and reports whether the shell asked to quit.
//
// Every command is waited on, because a deadline that gives up on a slow
// answer drops it under load and the assertion after reads a screen the answer
// never reached (the flake drive had, fixed in c009f36). The commands run at
// once, as the program loop runs them, and their answers reach Update one at a
// time on the test goroutine. A tick that only schedules the next one (the
// review's artifact watcher, the queue's wait glyph) is dropped once it
// answers, since feeding it back would never let the queue empty.
func pump(t *testing.T, s *tui.Shell, cmd tea.Cmd) bool {
	t.Helper()

	const deadlock = 10 * time.Second

	answers := make(chan tea.Msg)
	pending := 0
	start := func(c tea.Cmd) {
		if c == nil {
			return
		}

		pending++

		go func() { answers <- c() }()
	}

	start(cmd)

	quit := false
	deadline := time.After(deadlock)

	for pending > 0 {
		var msg tea.Msg

		select {
		case msg = <-answers:
			pending--
		case <-deadline:
			t.Fatalf("%d commands did not answer within %s", pending, deadlock)
		}

		switch msg := msg.(type) {
		case nil:
		case tea.QuitMsg:
			quit = true
		case tea.BatchMsg:
			for _, c := range msg {
				start(c)
			}
		default:
			if tui.Recurs(msg) {
				continue
			}

			_, next := s.Update(msg)
			start(next)
		}
	}

	return quit
}

// sendTo presses a key and runs what it asked for, reporting a quit.
func sendTo(t *testing.T, s *tui.Shell, k tea.KeyPressMsg) bool {
	t.Helper()

	_, cmd := s.Update(k)

	return pump(t, s, cmd)
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func letter(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// chooses is a queue whose every action leaves the screen, which is what
// opening a review is.
func chooses(t *testing.T) *tui.List {
	t.Helper()

	return list(t, func(_ tui.Action, r *tui.Row) (string, bool, error) {
		return "opening " + r.Key, true, nil
	})
}

// The queue and the review are one program. Opening a row draws the review over
// the queue that chose it, and leaving the review puts that same queue back:
// the filter it was narrowed to is still on, because nothing was built again.
func TestShellOpensAReviewAndComesBackToTheSameQueue(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 16, "check err"))

	l := chooses(t)
	typeList(l, "/questions")
	l.Update(enter())

	opens := 0
	s := tui.NewShell(t.Context(), l, func() tui.Reviewer {
		return func(context.Context) (*tui.Model, error) {
			opens++

			return m, nil
		}
	})

	pump(t, s, s.Init())
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if strings.Contains(plain(s.Frame()), parsed) {
		t.Fatalf("the review was drawn before a row was chosen:\n%s", s.Frame())
	}

	if quit := sendTo(t, s, enter()); quit {
		t.Fatal("choosing a row quit the program")
	}

	if got := plain(s.Frame()); !strings.Contains(got, parsed) {
		t.Fatalf("the review is not on screen:\n%s", got)
	}

	if quit := sendTo(t, s, letter('q')); quit {
		t.Fatal("leaving the review quit the program rather than the screen")
	}

	frame := plain(s.Frame())
	if !strings.Contains(frame, "showing 1 of 3") {
		t.Errorf("the queue came back without the filter it was narrowed to:\n%s", frame)
	}

	if opens != 1 {
		t.Errorf("the review was opened %d times, want once", opens)
	}
}

// A chosen row the caller will not open in place: either it means work the
// terminal has to be given back for, or it could not be read at all.
func TestShellAnswersARowItCannotOpenInPlace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		open tui.Handoff
		quit bool
		want string
	}{
		{
			name: "the caller wants the terminal back",
			open: func() tui.Reviewer { return nil },
			quit: true,
		},
		{
			name: "the review could not be read",
			open: func() tui.Reviewer {
				return func(context.Context) (*tui.Model, error) { return nil, errNoSuchReview }
			},
			want: errNoSuchReview.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := tui.NewShell(t.Context(), chooses(t), tc.open)
			pump(t, s, s.Init())

			if quit := sendTo(t, s, enter()); quit != tc.quit {
				t.Fatalf("choosing the row quit: %v, want %v", quit, tc.quit)
			}

			if tc.want == "" {
				return
			}

			if got := plain(s.Frame()); !strings.Contains(got, tc.want) {
				t.Errorf("the queue does not say %q:\n%s", tc.want, got)
			}
		})
	}
}

// C is answered outside the program, because moving a working copy asks about
// uncommitted work on stdin and two programs cannot own the terminal at once.
func TestShellQuitsForACheckout(t *testing.T) {
	t.Parallel()

	m := treeFixture(t, tui.TreeElsewhere)

	s := tui.NewShell(t.Context(), chooses(t), func() tui.Reviewer {
		return func(context.Context) (*tui.Model, error) { return m, nil }
	})

	pump(t, s, s.Init())
	sendTo(t, s, enter())

	if quit := sendTo(t, s, letter('C')); !quit {
		t.Fatalf("C did not quit the shell:\n%s", s.Frame())
	}

	if !s.Outcome().Checkout {
		t.Error("the shell was left without asking for the checkout")
	}
}

// A review named on the command line has no queue behind it, so leaving it ends
// the program rather than dropping into a screen that is not there. Esc asks for
// the queue in the Outcome, but only once the legend it would close is down. Esc
// then q typed quickly arrives as alt+q, and closes the legend and leaves all the same.
func TestReviewShellQuitsWithNothingBehindIt(t *testing.T) {
	t.Parallel()

	esc, altQ := tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt}

	for _, tc := range []struct {
		name  string
		first []tea.KeyPressMsg
		quit  tea.KeyPressMsg
		queue bool
	}{
		{"q", nil, letter('q'), false},
		{"esc then q from the legend", []tea.KeyPressMsg{letter('?')}, altQ, false},
		{"esc asks for the queue", nil, esc, true},
		{"esc after the legend closed", []tea.KeyPressMsg{letter('?'), esc}, esc, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 16, "check err"))

			s := tui.NewReviewShell(t.Context(), m)
			pump(t, s, s.Init())
			s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

			if got := plain(s.Frame()); !strings.Contains(got, parsed) {
				t.Fatalf("the review is not on screen:\n%s", got)
			}

			for _, k := range tc.first {
				if sendTo(t, s, k) {
					t.Fatalf("%s left the program before %s was pressed", tc.name, tc.quit.String())
				}
			}

			if quit := sendTo(t, s, tc.quit); !quit {
				t.Errorf("%s did not leave the program:\n%s", tc.name, plain(s.Frame()))
			}

			if got := s.Outcome().Queue; got != tc.queue {
				t.Errorf("%s: Outcome.Queue = %v, want %v", tc.name, got, tc.queue)
			}
		})
	}
}

// Esc backs out one level and never ends the program: at the queue, and at a
// review with no queue to open, it says q quits instead, so esc pressed to
// dismiss something does not end the sitting.
func TestEsc_NeverQuitsARoot(t *testing.T) {
	t.Parallel()

	esc := tea.KeyPressMsg{Code: tea.KeyEscape}

	for _, tc := range []struct {
		name  string
		shell func(t *testing.T) *tui.Shell
	}{
		{"the queue", func(t *testing.T) *tui.Shell {
			t.Helper()

			return tui.NewShell(t.Context(), chooses(t), nil)
		}},
		{"a local review", func(t *testing.T) *tui.Shell {
			t.Helper()

			m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 16, "check err"))

			return tui.NewReviewShell(t.Context(), m).AtRoot()
		}},
	} {
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 40}} {
			t.Run(fmt.Sprintf("%s/%dx%d", tc.name, size.Width, size.Height), func(t *testing.T) {
				t.Parallel()

				s := tc.shell(t)
				pump(t, s, s.Init())
				s.Update(size)

				if sendTo(t, s, esc) {
					t.Fatalf("esc quit %s", tc.name)
				}

				if got := plain(s.Frame()); !strings.Contains(got, "q quits") {
					t.Errorf("esc at %s does not say q quits:\n%s", tc.name, got)
				}

				if s.Outcome().Queue {
					t.Error("esc asked for a queue there is none of")
				}

				if !sendTo(t, s, letter('q')) {
					t.Errorf("q did not quit %s", tc.name)
				}
			})
		}
	}
}
