package tui_test

import (
	"maps"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/tui"
)

func places(section string, keys ...string) []tui.Place {
	out := make([]tui.Place, len(keys))
	for i, k := range keys {
		out[i] = tui.Place{Key: k, Section: section}
	}

	return out
}

func TestMovesNamesTheRowsThatMovedAndNotTheOnesThatSlid(t *testing.T) {
	t.Parallel()

	join := func(parts ...[]tui.Place) []tui.Place {
		var out []tui.Place
		for _, p := range parts {
			out = append(out, p...)
		}

		return out
	}

	for _, c := range []struct {
		name      string
		old, next []tui.Place
		want      map[string]tui.Move
	}{
		{"same order", places("a", "1", "2", "3"), places("a", "1", "2", "3"), map[string]tui.Move{}},
		{"nothing before", nil, places("a", "1", "2"), map[string]tui.Move{"1": tui.MoveNew, "2": tui.MoveNew}},
		{"a new row", places("a", "1", "2"), places("a", "1", "9", "2"), map[string]tui.Move{"9": tui.MoveNew}},
		{"a row leaves", places("a", "1", "2", "3"), places("a", "2", "3"), map[string]tui.Move{}},
		{
			"a row jumps to the top", places("a", "1", "2", "3", "4"), places("a", "4", "1", "2", "3"),
			map[string]tui.Move{"4": tui.MoveRank},
		},
		{
			"a row drops to the bottom", places("a", "1", "2", "3"), places("a", "2", "3", "1"),
			map[string]tui.Move{"1": tui.MoveRank},
		},
		{"a swap", places("a", "1", "2"), places("a", "2", "1"), map[string]tui.Move{"2": tui.MoveRank}},
		{
			"a lane change leaves the rows around it where they were",
			join(places("a", "1", "2", "3"), places("b", "4")),
			join(places("a", "1", "3"), places("b", "2", "4")),
			map[string]tui.Move{"2": tui.MoveLane},
		},
		{
			"a lane that empties into another",
			join(places("a", "1"), places("b", "2")), join(places("b", "1", "2")),
			map[string]tui.Move{"1": tui.MoveLane},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := tui.Moves(c.old, c.next); !maps.Equal(got, c.want) {
				t.Errorf("moves = %v, want %v", got, c.want)
			}
		})
	}
}

// clock is a time the test sets, so a hold is crossed by saying so rather than
// by waiting.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func (c *clock) pass(d time.Duration) { c.at = c.at.Add(d) }

// swap is a loader whose answer is a new order of rows.
type swap struct{ keys []string }

type swapped []string

func (*swap) Start() tea.Cmd { return nil }

func (s *swap) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	got, ok := msg.(swapped)
	if ok {
		s.keys = got
	}

	return nil, ok
}

func (s *swap) sections() []tui.Section {
	rows := make([]tui.Row, len(s.keys))
	for i, k := range s.keys {
		rows[i] = tui.Row{Key: k, Left: "repo#" + k}
	}

	return []tui.Section{{Name: "lane", Rows: rows}}
}

func reordering(t *testing.T, keys ...string) (*tui.List, *clock) {
	t.Helper()

	s := &swap{keys: keys}
	c := &clock{at: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}

	l := tui.NewList("inbox", s.sections, func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil }).
		WithLoader(s)
	l.SetClock(c.now)
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	return l, c
}

// drawnOrder is the three rows the tests draw, which stops short of the
// inspector repeating the row under the cursor.
func drawnOrder(l *tui.List) []string {
	const n = 3

	var out []string

	for _, line := range strings.Split(plain(l.ListFrame()), "\n") {
		if i := strings.Index(line, "repo#"); i >= 0 {
			out = append(out, line[i+len("repo#"):i+len("repo#")+1])
		}
	}

	return out[:min(n, len(out))]
}

func tap(l *tui.List, r rune) { l.Update(tea.KeyPressMsg{Code: r, Text: string(r)}) }

// A refreshed order that lands in the middle of a burst of keys is held until
// the keyboard has been quiet, every key in the burst is handled as it comes,
// and the cursor is on the same row when the order is finally drawn.
func TestARefreshedOrderWaitsOutAKeyBurstAndTheCursorKeepsItsRow(t *testing.T) {
	t.Parallel()

	l, c := reordering(t, "A", "B", "C")

	tap(l, 'j')
	c.pass(100 * time.Millisecond)
	l.Update(swapped{"C", "B", "A"})

	if !l.Held() || strings.Join(drawnOrder(l), "") != "ABC" {
		t.Fatalf("held=%v order=%v: want the new order held back while keys are arriving", l.Held(), drawnOrder(l))
	}

	c.pass(50 * time.Millisecond)
	tap(l, 'j')

	if got := l.CursorKey(); got != "C" {
		t.Fatalf("the key was delayed by the hold: the cursor is on %q, want C", got)
	}

	c.pass(300 * time.Millisecond)
	l.Update(tui.HoldTick())

	if !l.Held() || strings.Join(drawnOrder(l), "") != "ABC" {
		t.Fatalf("held=%v order=%v: want the wait to start over from the second key", l.Held(), drawnOrder(l))
	}

	c.pass(150 * time.Millisecond)
	l.Update(tui.HoldTick())

	if l.Held() || strings.Join(drawnOrder(l), "") != "CBA" {
		t.Fatalf("held=%v order=%v: want the new order once the keyboard was quiet", l.Held(), drawnOrder(l))
	}

	if got := l.CursorKey(); got != "C" {
		t.Errorf("the cursor is on %q after the apply, want the row it was on", got)
	}

	if got, want := l.Shifts(), map[string]tui.Move{"B": tui.MoveRank, "C": tui.MoveRank}; !maps.Equal(got, want) {
		t.Errorf("shifts = %v, want %v", got, want)
	}
}

// With no key in the last 400 ms the answer is drawn at once. A cursor the
// reader never moved stays on the top row, which is whichever row that is now.
func TestAnIdleKeyboardTakesTheNewOrderAtOnce(t *testing.T) {
	t.Parallel()

	l, c := reordering(t, "A", "B", "C")

	l.Update(swapped{"C", "A", "B"})

	if l.Held() || strings.Join(drawnOrder(l), "") != "CAB" {
		t.Fatalf("held=%v order=%v: want the order drawn at once", l.Held(), drawnOrder(l))
	}

	if got := l.CursorKey(); got != "C" {
		t.Errorf("an untouched cursor is on %q, want the top row", got)
	}

	tap(l, 'j')
	c.pass(time.Second)
	l.Update(swapped{"B", "C", "A"})

	if l.Held() || l.CursorKey() != "A" {
		t.Errorf("held=%v cursor=%q: want the order drawn with the cursor still on A", l.Held(), l.CursorKey())
	}
}
