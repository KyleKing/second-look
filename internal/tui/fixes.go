package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/brief"
	"github.com/kyleking/second-look/internal/threads"
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

// Hand is who gets the fixes behind a posted todo reply.
type Hand int

// The hand-offs a posted todo reply can end in.
const (
	HandNobody Hand = iota
	HandWatchDoggo
	HandLocal
)

var handNames = [...]string{HandNobody: "nobody", HandWatchDoggo: "watch-doggo", HandLocal: "local"}

func (h Hand) String() string { return handNames[h] }

// HandNames lists the names ParseHand takes, as a usage line spells them.
func HandNames() string {
	return handNames[HandWatchDoggo] + "|" + handNames[HandLocal] + "|" + handNames[HandNobody]
}

// ErrUnknownHand is a name ParseHand does not know.
var ErrUnknownHand = errors.New("unknown hand-off")

// ParseHand reads the name a hand-off goes by on a command line.
func ParseHand(name string) (Hand, error) {
	i := slices.Index(handNames[:], name)
	if i < 0 {
		return HandNobody, fmt.Errorf("%w %q, want %s", ErrUnknownHand, name, HandNames())
	}

	return Hand(i), nil
}

func fixTargets(def string) [][2]string {
	return [][2]string{
		{"w", HandWatchDoggo.String()}, {"l", "local claude"}, {"n", HandNobody.String()}, {"enter", def},
	}
}

func (m *Model) defaultHand() (Hand, string) {
	if m.fixes.ToWatchDoggo && m.fixes.Add != nil {
		return HandWatchDoggo, HandWatchDoggo.String()
	}

	return HandLocal, "local claude"
}

// askFixes puts up the hand-off choice that every post carrying a todo reply
// stops at; nothing posts until it names who gets the fixes.
func (m *Model) askFixes(lead string) {
	m.asking = askFixes

	_, def := m.defaultHand()
	m.say(lead+"  fixes go to  "+hintLine(fixTargets(def)), false)
}

// askFixesTo is the submit chord's extra step, reached only with a todo reply.
func (m *Model) askFixesTo(event, chosen string) {
	m.fixEvent = event
	m.askFixes(fmt.Sprintf("S %s  %d todo", chosen, len(m.review.TodoReplies())))
}

func (m *Model) answerFixes(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	event, one := m.fixEvent, m.fixOne
	m.fixOne = ""

	var hand Hand

	switch {
	case msg.String() == "w":
		hand = HandWatchDoggo
	case msg.String() == "l":
		hand = HandLocal
	case msg.String() == "n":
		hand = HandNobody
	case key.Matches(msg, m.keys.Accept):
		hand, _ = m.defaultHand()
	default:
		m.say("canceled, nothing was posted", false)

		return m, nil
	}

	if hand == HandWatchDoggo && m.fixes.Add == nil {
		m.say("handing fixes to Watch Doggo is not available here", true)

		return m, nil
	}

	if one != "" {
		cmd := m.sendWith(one, hand)

		return m, cmd
	}

	return m.submitWith(event, hand)
}

// Delivery is what a hand-off needs beyond the review it follows: where the
// set of fixes is written, what runs over it, and how the pull request is labeled.
type Delivery struct {
	Fixes

	Store string
	Run   Dispatcher
}

func (m *Model) handOff(ctx context.Context, hand Hand, review *artifact.Review) func() (string, error) {
	return HandOff(ctx, hand, Delivery{Fixes: m.fixes, Store: m.store, Run: m.dispatcher}, review, m.threads)
}

// HandOff is what follows a posted review: the label, or the findings handed
// to the local agent. It is nil where the replies post and nothing else does.
// The findings are read when it is built, so it can be built before the post
// clears what they come from.
func HandOff(
	ctx context.Context, hand Hand, d Delivery, review *artifact.Review, ts []threads.Thread,
) func() (string, error) {
	switch hand {
	case HandWatchDoggo:
		add, label := d.Add, d.Label

		return func() (string, error) { return label + " added", add(ctx, review, label) }
	case HandLocal:
		n, text := len(review.TodoReplies()), brief.Fixes(review, ts)
		store, session, run := d.Store, review.Agent.Session, d.Run

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
	case HandNobody:
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
