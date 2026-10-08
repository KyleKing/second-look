package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/blame"
	"github.com/kyleking/second-look/internal/diff"
)

// blameMsg is the checkout's answer to how old each line is, asked once and
// kept: a second u b is a redraw, not another walk of the history.
type blameMsg struct {
	aged blame.Map
	err  error
}

// toggleBlame answers u then b. The column needs the history read before it
// can be drawn, so the first ask runs behind the frame and the ones after it
// cost nothing.
func (m *Model) toggleBlame() tea.Cmd {
	if m.drawn.blame {
		m.drawn.blame = false
		m.rebuild()
		m.sayLook()

		return nil
	}

	// The seam is only set where a checkout exists, since blame reads the
	// repository's own objects rather than the forge's copy.
	if m.blamer == nil {
		m.say(m.noTree(), true)

		return nil
	}

	if m.blamed != nil {
		m.drawn.blame = true
		m.rebuild()
		m.sayLook()

		return nil
	}

	if m.blaming {
		m.say("blame is still reading the history", false)

		return nil
	}

	m.blaming = true
	m.say("reading the history…", false)

	ask := m.blamer

	return func() tea.Msg {
		aged, err := ask(context.Background())

		return blameMsg{aged: aged, err: err}
	}
}

// applyBlame draws the column once the history answers, or says why it could
// not: a checkout shallow enough to lack the merge base gets the refusal
// rather than a column of nothing.
func (m *Model) applyBlame(msg blameMsg) {
	m.blaming = false

	if msg.err != nil {
		m.say("blaming: "+msg.err.Error(), true)

		return
	}

	m.blamed, m.blamedAt = msg.aged, time.Now()
	m.drawn.blame = true
	m.rebuild()
	m.sayLook()
}

// ageRamp is the column's five rungs, from touched today to touched years
// ago. The glyph carries the bucket where color cannot, and today draws
// nothing: a line the change itself wrote is as fresh as there is, and the
// sign column already says which those are.
var ageRamp = []string{" ", "░", "▒", "▓", "█"}

// The rungs of the ramp, youngest to oldest.
const (
	ageToday = iota
	ageWeek
	ageMonth
	ageYear
	ageOlder
)

// ageUnknown marks a line blame could not answer for, so it reads as no
// answer rather than as a fresh one.
const ageUnknown = "·"

func ageBucket(when, now time.Time) int {
	const day = 24 * time.Hour

	switch d := now.Sub(when); {
	case d < day:
		return ageToday
	case d < 7*day:
		return ageWeek
	case d < 30*day:
		return ageMonth
	case d < 365*day:
		return ageYear
	default:
		return ageOlder
	}
}

// blameCell is the one glyph of the column: the age of the line's last change
// on the old side, blank for a line the change itself wrote, and · for one
// history had nothing to say about.
func (m *Model) blameCell(path string, l diff.Line) string {
	if l.Old == 0 {
		return " "
	}

	found, ok := m.blamed[path][l.Old]
	if !ok {
		return m.rich.gutter.Render(ageUnknown)
	}

	bucket := ageBucket(found.When, m.blamedAt)

	return m.rich.age[bucket].Render(ageRamp[bucket])
}
