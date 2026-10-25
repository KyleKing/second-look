package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/brief"
)

// Labeler adds a label to the pull request a review belongs to.
type Labeler func(ctx context.Context, r *artifact.Review, label string) error

// Fixes says where the todo replies of a posted review go. Label is what marks
// the pull request for Watch Doggo, and ToWatchDoggo is where enter sends them.
type Fixes struct {
	Add          Labeler
	Label        string
	ToWatchDoggo bool
}

// WithFixes lets the submit step hand todo replies to Watch Doggo. Without it
// w says so rather than appearing to work.
func WithFixes(f Fixes) Option {
	return func(m *Model) { m.fixes = f }
}

type handoff int

const (
	handNobody handoff = iota
	handWatchDoggo
	handLocal
)

func fixTargets(def string) [][2]string {
	return [][2]string{
		{"w", "watch-doggo"}, {"l", "local claude"}, {"n", "nobody"}, {"enter", def},
	}
}

func (m *Model) defaultHand() (handoff, string) {
	if m.fixes.ToWatchDoggo && m.fixes.Add != nil {
		return handWatchDoggo, "watch-doggo"
	}

	return handLocal, "local claude"
}

// askFixesTo is the submit chord's extra step, reached only with a todo reply;
// nothing posts until it names who gets the fixes.
func (m *Model) askFixesTo(event, chosen string) {
	m.fixEvent, m.asking = event, askFixes

	_, def := m.defaultHand()
	m.say(fmt.Sprintf("S %s  %d todo  fixes go to  %s", chosen, len(m.review.TodoReplies()),
		hintLine(fixTargets(def))), false)
}

func (m *Model) answerFixes(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	event := m.fixEvent

	var hand handoff

	switch {
	case msg.String() == "w":
		hand = handWatchDoggo
	case msg.String() == "l":
		hand = handLocal
	case msg.String() == "n":
		hand = handNobody
	case key.Matches(msg, m.keys.Accept):
		hand, _ = m.defaultHand()
	default:
		m.say("canceled, nothing was posted", false)

		return m, nil
	}

	if hand == handWatchDoggo && m.fixes.Add == nil {
		m.say("handing fixes to Watch Doggo is not available here", true)

		return m, nil
	}

	return m.submitWith(event, hand)
}

// handOff is what follows a posted review: the label, or the findings handed
// to the local agent. It is nil where the replies post and nothing else does.
func (m *Model) handOff(ctx context.Context, hand handoff) func() (string, error) {
	review := m.review

	switch hand {
	case handWatchDoggo:
		add, label := m.fixes.Add, m.fixes.Label

		return func() (string, error) { return label + " added", add(ctx, review, label) }
	case handLocal:
		n, text := len(review.TodoReplies()), brief.Fixes(review, m.threads)
		store, session, run := m.store, review.Agent.Session, m.dispatcher

		return func() (string, error) {
			path, err := writeSetTo(store, review.Number, text)
			if err != nil {
				return "", err
			}

			if run == nil {
				return unread(n, path), nil
			}

			if _, err := run(ctx, path, session); err != nil {
				return "", fmt.Errorf("dispatching: %w", err)
			}

			return "handed " + plural(n, "todo") + " to the local agent", nil
		}
	case handNobody:
	}

	return nil
}

// withHandOff runs the hand-off once the review has posted. A failure is part
// of the line rather than an error, because the replies are already on GitHub.
func withHandOff(summary string, handOff func() (string, error)) (string, error) {
	if handOff == nil {
		return summary, nil
	}

	note, err := handOff()
	if err != nil {
		return summary + " · hand-off failed: " + oneLine(err), err
	}

	return summary + " · " + note, nil
}

func oneLine(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
