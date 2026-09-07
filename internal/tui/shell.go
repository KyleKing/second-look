package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Reviewer opens the review the queue was left on. It runs as a command
// because the open reaches the network, so the queue stays drawn while it is
// out.
type Reviewer func(ctx context.Context) (*Model, error)

// Handoff is what the queue was left for, answered by the caller out of
// whatever its own screens recorded. A nil Reviewer quits the shell, which is
// how an action needing the terminal back is performed: a checkout asks about
// uncommitted work on stdin, and two programs cannot own the terminal at once.
type Handoff func() Reviewer

// mode is which screen the shell is drawing.
type mode int

const (
	modeList mode = iota
	// An open in flight keeps the queue on screen, and drops the keys pressed
	// through it: a second enter would put a second open in flight.
	modeOpening
	modeReview
)

// Shell is the queue and the review screen in one program.
//
// They were two, so reading a review tore the queue down and coming back built
// it again: every search paid for twice, and the filter and the cursor lost in
// between.
type Shell struct {
	ctx  context.Context //nolint:containedctx // it bounds the open a chosen row makes
	list *List
	// review is the screen being read, nil until a row is opened and again once
	// it is left.
	review *Model
	open   Handoff
	at     mode

	width  int
	height int
	// failure is what a screen the shell has already left could not finish,
	// which a footer the alternate screen took with it is not.
	failure error
}

// NewShell composes the queue and the review screen, so opening a row keeps
// the queue that was behind it.
func NewShell(ctx context.Context, l *List, open Handoff) *Shell {
	return &Shell{ctx: ctx, list: l, open: open, width: minWidth, height: startHeight}
}

// NewReviewShell is the same shell holding one review and no queue, which is
// what a pull request named on the command line opens. Leaving the review ends
// the program, there being nothing behind it.
func NewReviewShell(ctx context.Context, m *Model) *Shell {
	return &Shell{ctx: ctx, review: m, at: modeReview, width: minWidth, height: startHeight}
}

// chosenMsg is a list action that asked to leave the screen, which the shell
// answers by asking its caller what that row meant.
type chosenMsg struct{}

// leftMsg is the review screen asking for whatever is behind it. Whether that
// ends the program is the shell's decision rather than the screen's.
type leftMsg struct{}

// openedMsg is what the open answered with.
type openedMsg struct {
	review *Model
	err    error
}

// Init starts whichever screen the shell opens on.
func (s *Shell) Init() tea.Cmd {
	if s.at == modeReview {
		return s.review.Init()
	}

	return s.list.Init()
}

// Update routes one message. Keys reach the screen being read; everything else
// reaches both, since a search answering while a review is open belongs to the
// queue behind it and would otherwise be dropped.
func (s *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		cmd := s.both(msg)

		return s, cmd
	case chosenMsg:
		cmd := s.opening()

		return s, cmd
	case openedMsg:
		cmd := s.opened(msg)

		return s, cmd
	case leftMsg:
		cmd := s.back()

		return s, cmd
	case tea.KeyPressMsg:
		cmd := s.key(msg)

		return s, cmd
	}

	cmd := s.both(msg)

	return s, cmd
}

func (s *Shell) both(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	if s.list != nil {
		_, cmd := s.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	if s.review != nil {
		_, cmd := s.review.Update(msg)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (s *Shell) key(msg tea.KeyPressMsg) tea.Cmd {
	switch s.at {
	case modeOpening:
		return nil
	case modeReview:
		_, cmd := s.review.Update(msg)

		return cmd
	case modeList:
		_, cmd := s.list.Update(msg)

		return cmd
	}

	return nil
}

// opening asks the caller what the chosen row meant. A row that means
// something the terminal has to be given back for quits instead.
func (s *Shell) opening() tea.Cmd {
	if s.open == nil {
		return tea.Quit
	}

	open := s.open()
	if open == nil {
		return tea.Quit
	}

	s.at = modeOpening
	ctx := s.ctx

	return func() tea.Msg {
		m, err := open(ctx)

		return openedMsg{review: m, err: err}
	}
}

// opened puts the review on screen, or reports why it could not be read and
// leaves the queue where it was.
func (s *Shell) opened(msg openedMsg) tea.Cmd {
	if msg.err != nil {
		s.at = modeList
		s.list.say(msg.err.Error(), true)

		return nil
	}

	s.review, s.at = msg.review, modeReview
	// The screen is built at the assumed size, and no resize follows an open
	// that nothing outside the program made.
	s.review.Update(tea.WindowSizeMsg{Width: s.width, Height: s.height})

	return tea.Batch(s.review.Init(), tea.ClearScreen)
}

// back leaves the review for the queue behind it, which is the queue that was
// there: nothing is searched again and the filter is where it was left.
func (s *Shell) back() tea.Cmd {
	// A submit that failed is not un-failed by the next review going well, so
	// it is kept for the exit code as well as said once in the queue's footer.
	failed := s.review.failure
	if failed != nil {
		s.failure = failed
	}

	if s.list == nil {
		return tea.Quit
	}

	s.review, s.at = nil, modeList

	if failed != nil {
		s.list.say(failed.Error(), true)
	}

	return tea.ClearScreen
}

// View draws the screen being read. An open in flight keeps the queue on
// screen, whose footer says which row is being opened.
func (s *Shell) View() tea.View {
	if s.at == modeReview {
		return s.review.View()
	}

	return s.list.View()
}

// Outcome is what the shell was left through, empty for a session that ended
// on the queue.
func (s *Shell) Outcome() Outcome {
	if s.review == nil {
		return Outcome{}
	}

	return Outcome{Checkout: s.review.checkout, Next: s.review.next}
}

// Failure is the action that did not complete, so a submit that failed reaches
// stdout and the exit code rather than only a footer the screen took with it.
func (s *Shell) Failure() error {
	switch {
	case s.review != nil && s.review.failure != nil:
		return s.review.failure
	case s.failure != nil:
		return s.failure
	case s.list != nil:
		return s.list.failure
	}

	return nil
}

// RunShell opens the session and blocks until the person leaves it. Every
// change is written to the artifact as it is made, so quitting loses nothing
// and a crash loses only the keystroke in flight.
func RunShell(s *Shell) (Outcome, error) {
	final, err := tea.NewProgram(s).Run()
	if err != nil {
		return Outcome{}, fmt.Errorf("running second-look: %w", err)
	}

	got, ok := final.(*Shell)
	if !ok {
		return Outcome{}, nil
	}

	return got.Outcome(), got.Failure()
}
