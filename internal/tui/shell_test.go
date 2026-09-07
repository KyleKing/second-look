package tui_test

import (
	"context"
	"errors"
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
// loop does, and reports whether the shell asked to quit.
//
// A command that has not answered quickly is dropped rather than waited on: the
// review screen's watcher and the queue's settle timer both reschedule forever,
// and the program loop runs them in the background anyway.
func pump(t *testing.T, s *tui.Shell, cmd tea.Cmd) bool {
	t.Helper()

	if cmd == nil {
		return false
	}

	const patience = 100 * time.Millisecond

	answered := make(chan tea.Msg, 1)
	go func() { answered <- cmd() }()

	var msg tea.Msg

	select {
	case msg = <-answered:
	case <-time.After(patience):
		return false
	}

	switch msg := msg.(type) {
	case nil:
		return false
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		quit := false

		for _, c := range msg {
			quit = pump(t, s, c) || quit
		}

		return quit
	default:
		_, next := s.Update(msg)

		return pump(t, s, next)
	}
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
// the program rather than dropping into a screen that is not there.
func TestReviewShellQuitsWithNothingBehindIt(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 16, "check err"))

	s := tui.NewReviewShell(t.Context(), m)
	pump(t, s, s.Init())
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if got := plain(s.Frame()); !strings.Contains(got, parsed) {
		t.Fatalf("the review is not on screen:\n%s", got)
	}

	if quit := sendTo(t, s, letter('q')); !quit {
		t.Error("q did not leave the program")
	}
}
