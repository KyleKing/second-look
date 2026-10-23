package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// spot is where the cursor stood: enough to put the screen back exactly, down
// to the scroll offset and the card a peek had open.
type spot struct {
	view   viewMode
	whole  *openFile
	peeked *peekCard
	cursor int
	offset int
	was    where
}

// jumpList is the spots a jump left, walked back with ctrl-o and forward with
// ctrl-i. The index at is the spot being stood on while walking, and the length
// of spots when not walking.
type jumpList struct {
	spots []spot
	at    int
}

// push records where a jump left from. A jump made after walking back drops
// the spots ahead of the one it left, the way a browser's history does.
func (j *jumpList) push(from spot) {
	j.spots = append(j.spots[:j.at], from)
	j.at = len(j.spots)
}

// back is the spot before the one being stood on. Standing at the end of the
// list, here is kept as the spot forward comes back to.
func (j *jumpList) back(here spot) (spot, bool) {
	if j.at == 0 {
		return spot{}, false
	}

	if j.at == len(j.spots) {
		j.spots = append(j.spots, here)
	}

	j.at--

	return j.spots[j.at], true
}

func (j *jumpList) forward() (spot, bool) {
	if j.at+1 >= len(j.spots) {
		return spot{}, false
	}

	j.at++

	return j.spots[j.at], true
}

// ahead reports whether a ctrl-o has left spots to walk forward to.
func (j *jumpList) ahead() bool { return j.at+1 < len(j.spots) }

func (m *Model) snapshot() spot {
	return spot{
		view: m.view, whole: m.whole, peeked: m.peeked,
		cursor: m.cursor, offset: m.offset, was: m.here(),
	}
}

// jumpedFrom records where a jump left from, once the jump has moved the cursor.
func (m *Model) jumpedFrom(from spot) {
	if from.cursor != m.cursor || from.view != m.view {
		m.jumps.push(from)
	}
}

// restore puts the screen back where a spot left it: the same view, the same
// card, the row it stood on and the scroll it had.
func (m *Model) restore(p spot) {
	m.view, m.whole, m.peeked = p.view, p.whole, p.peeked
	m.rebuild()

	m.cursor = clamp(p.cursor, len(m.screen.rows)-1)
	if !same(m.screen.rows[m.cursor], p.was) {
		if i := m.find(p.was); i >= 0 {
			m.cursor = i
		}
	}

	m.offset = clamp(p.offset, m.maxOffset())
}

// walked answers the keys that walk the jump list, which tab joins once ctrl+o
// has left a place ahead.
func (m *Model) walked(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.JumpBack):
		m.walkJumps(-1)
	case key.Matches(msg, m.keys.JumpForward), key.Matches(msg, m.keys.NextNote) && m.jumps.ahead():
		m.walkJumps(1)
	default:
		return false
	}

	return true
}

// walkJumps goes back or forward through the jump list.
func (m *Model) walkJumps(direction int) {
	var (
		to   spot
		went bool
	)

	if direction < 0 {
		to, went = m.jumps.back(m.snapshot())
	} else {
		to, went = m.jumps.forward()
	}

	if !went {
		if direction < 0 {
			m.say("no earlier place to go back to", false)
		} else {
			m.say("no later place; ctrl+o walks back first", false)
		}

		return
	}

	m.restore(to)
	m.say("", false)
}
