package tui

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
)

// idleAfter is how long the keyboard must be quiet before a refreshed order is
// drawn, so rows never shift in the middle of a burst of j.
const idleAfter = 400 * time.Millisecond

// holdMsg is the wait for the keyboard to go quiet running out.
type holdMsg struct{}

func holdFor(wait time.Duration) tea.Cmd {
	return tea.Tick(wait, func(time.Time) tea.Msg { return holdMsg{} })
}

// Place is where a row sits in a list: its key and the section it is under.
type Place struct{ Key, Section string }

// Move is how a row differs between two orders.
type Move uint8

// The ways a row can differ. A row that kept its lane and its place among the
// rows around it has none.
const (
	// MoveNew is a row the earlier order did not hold.
	MoveNew Move = iota + 1
	// MoveLane is a row that changed section.
	MoveLane
	// MoveRank is a row that stayed in its section and changed place in it.
	MoveRank
)

// Moves says which rows of next differ from old. A row that only slid because
// others left or arrived is not one: within a section, the rows that kept their
// relative order are the longest run of them, and each row outside it moved.
func Moves(old, next []Place) map[string]Move {
	was := make(map[string]Place, len(old))
	at := make(map[string]int, len(old))

	for i, p := range old {
		was[p.Key], at[p.Key] = p, i
	}

	out := map[string]Move{}
	kept := map[string][]string{}

	for _, p := range next {
		switch before, ok := was[p.Key]; {
		case !ok:
			out[p.Key] = MoveNew
		case before.Section != p.Section:
			out[p.Key] = MoveLane
		default:
			kept[p.Section] = append(kept[p.Section], p.Key)
		}
	}

	for _, keys := range kept {
		pos := make([]int, len(keys))
		for i, k := range keys {
			pos[i] = at[k]
		}

		for i, still := range inOrder(pos) {
			if !still {
				out[keys[i]] = MoveRank
			}
		}
	}

	return out
}

// inOrder marks a longest strictly increasing run of pos.
func inOrder(pos []int) []bool {
	keep := make([]bool, len(pos))
	if len(pos) == 0 {
		return keep
	}

	var tails []int

	prev := make([]int, len(pos))

	for i, v := range pos {
		k := sort.Search(len(tails), func(k int) bool { return pos[tails[k]] >= v })

		prev[i] = -1
		if k > 0 {
			prev[i] = tails[k-1]
		}

		if k == len(tails) {
			tails = append(tails, i)
		} else {
			tails[k] = i
		}
	}

	for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
		keep[i] = true
	}

	return keep
}

// placesOf is the order the rows are drawn in. A folded section draws none.
func placesOf(sections []Section) []Place {
	var out []Place

	for i := range sections {
		if sections[i].Fold {
			continue
		}

		for j := range sections[i].Rows {
			out = append(out, Place{Key: sections[i].Rows[j].Key, Section: sections[i].Name})
		}
	}

	return out
}

func (l *List) clock() time.Time {
	if l.now == nil {
		return time.Now()
	}

	return l.now()
}

// untilIdle is how much longer the keyboard has to stay quiet.
func (l *List) untilIdle() time.Duration {
	return max(0, idleAfter-l.clock().Sub(l.lastKey))
}

// apply draws what a loader just answered, or holds it until the keyboard has
// been quiet for idleAfter. A key is never held.
func (l *List) apply() tea.Cmd {
	if l.held {
		return nil
	}

	if wait := l.untilIdle(); wait > 0 {
		l.held = true

		return holdFor(wait)
	}

	l.refresh()

	return nil
}

// idle answers the wait running out, and waits again where a key came meanwhile.
func (l *List) idle() tea.Cmd {
	if wait := l.untilIdle(); wait > 0 {
		return holdFor(wait)
	}

	l.held = false
	l.refresh()

	return l.rest()
}

// refresh redraws from what the loaders hold and notes which rows moved.
func (l *List) refresh() {
	was := l.baseline

	l.rebuild()
	l.shifts = Moves(was, l.baseline)
}
