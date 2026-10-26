package tui_test

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

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

// swap is a loader whose answer is a new order of rows. Its rows are Stale
// until it answers, as a snapshot's are.
type swap struct {
	keys  []string
	stale bool
}

type swapped []string

func (*swap) Start() tea.Cmd { return nil }

func (s *swap) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	got, ok := msg.(swapped)
	if ok {
		s.keys, s.stale = got, false
	}

	return nil, ok
}

func (s *swap) sections() []tui.Section {
	rows := make([]tui.Row, len(s.keys))
	for i, k := range s.keys {
		rows[i] = tui.Row{
			Key: k, Stale: s.stale,
			Cells: map[string]tui.Cell{
				"ref":   {Spans: []tui.Span{sp(tui.RoleCell, "repo#"+k)}},
				"title": {Spans: []tui.Span{sp(tui.RoleText, "title of "+k)}},
			},
		}
	}

	return []tui.Section{{Name: "lane", Rows: rows}}
}

func reordering(t *testing.T, stale bool, keys ...string) (*tui.List, *clock) {
	t.Helper()

	s := &swap{keys: keys, stale: stale}
	c := &clock{at: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}

	l := tui.NewTabs([]tui.Tab{{
		Name: "inbox", Sections: s.sections, Columns: laneColumns, Loader: s,
		Act: func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil },
	}}, 0)
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

	l, c := reordering(t, true, "A", "B", "C")

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

	if l.FlashGen() != 0 {
		t.Fatalf("rows flashed on the old order, before the answer was drawn")
	}

	c.pass(150 * time.Millisecond)
	l.Update(tui.HoldTick())

	if l.Held() || strings.Join(drawnOrder(l), "") != "CBA" {
		t.Fatalf("held=%v order=%v: want the new order once the keyboard was quiet", l.Held(), drawnOrder(l))
	}

	if got := l.CursorKey(); got != "C" {
		t.Errorf("the cursor is on %q after the apply, want the row it was on", got)
	}

	if l.Flash("C") != 4 || l.Flash("B") != 4 || l.Flash("A") != 0 {
		t.Errorf("flash after the apply: B=%d C=%d A=%d, want the moved rows at 4",
			l.Flash("B"), l.Flash("C"), l.Flash("A"))
	}

	if got, want := l.Shifts(), map[string]tui.Move{"B": tui.MoveRank, "C": tui.MoveRank}; !maps.Equal(got, want) {
		t.Errorf("shifts = %v, want %v", got, want)
	}
}

// With no key in the last 400 ms the answer is drawn at once. A cursor the
// reader never moved stays on the top row, which is whichever row that is now.
func TestAnIdleKeyboardTakesTheNewOrderAtOnce(t *testing.T) {
	t.Parallel()

	l, c := reordering(t, true, "A", "B", "C")

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

// decayed is the steps a flash passes through, read off the list after each
// tick, and whether the last tick asked for another.
func decayed(l *tui.List, key string) ([]uint8, bool) {
	steps := []uint8{l.Flash(key)}
	more := true

	for gen := l.FlashGen(); more && len(steps) < 10; {
		_, cmd := l.Update(tui.FlashTick(gen))
		more = cmd != nil

		steps = append(steps, l.Flash(key))
	}

	return steps, more
}

// A row the refresh moved starts at step four and loses one a tick, the timer
// stops asking for ticks once none is left, and a row that only slid, or that
// the refresh did not touch, never flashes.
func TestAMovedRowFlashesFourStepsAndTheTimerStops(t *testing.T) {
	t.Parallel()

	l, _ := reordering(t, true, "A", "B", "C")

	l.Update(swapped{"B", "A", "C", "D"})

	for key, want := range map[string]uint8{"B": 4, "D": 4, "A": 0, "C": 0} {
		if got := l.Flash(key); got != want {
			t.Errorf("%s is at step %d, want %d", key, got, want)
		}
	}

	steps, more := decayed(l, "B")
	if got := []uint8{4, 3, 2, 1, 0}; !slices.Equal(steps, got) || more {
		t.Errorf("B went %v (another tick asked for: %v), want %v and no further tick", steps, more, got)
	}

	if l.Flashing() != 0 {
		t.Errorf("%d rows still flash after the timer stopped", l.Flashing())
	}
}

// A tick belongs to the refresh that started it: a second refresh restarts the
// rows it moved, and the first one's tick lands on nothing.
func TestATickFromAnEarlierRefreshIsDropped(t *testing.T) {
	t.Parallel()

	l, _ := reordering(t, true, "A", "B", "C")

	l.Update(swapped{"B", "A", "C"})
	first := l.FlashGen()

	l.Update(tui.FlashTick(first))
	l.Update(swapped{"B", "C", "A"})

	if l.FlashGen() == first {
		t.Fatal("the second refresh reused the first one's tick number")
	}

	if _, cmd := l.Update(tui.FlashTick(first)); cmd != nil || l.Flash("B") != 3 || l.Flash("C") != 4 {
		t.Errorf("a tick from the first refresh left B at %d and C at %d or asked for another, want 3 and 4",
			l.Flash("B"), l.Flash("C"))
	}

	l.Update(tui.FlashTick(l.FlashGen()))

	if l.Flash("B") != 2 || l.Flash("C") != 3 {
		t.Errorf("the second refresh's tick left B at %d and C at %d, want 2 and 3", l.Flash("B"), l.Flash("C"))
	}
}

// Nothing is compared against a screen that never drew a snapshot, and an order
// that did not change has nothing to flash.
func TestNoFlashOnAColdOpenOrAnUnchangedOrder(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		stale bool
		next  []string
	}{
		{"cold open", false, []string{"B", "A", "C", "D"}},
		{"same order", true, []string{"A", "B", "C"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			l, _ := reordering(t, c.stale, "A", "B", "C")

			_, cmd := l.Update(swapped(c.next))
			if l.Flashing() != 0 || l.FlashGen() != 0 {
				t.Errorf("%d rows flash (tick %d), want none", l.Flashing(), l.FlashGen())
			}

			if cmd != nil {
				t.Error("a screen with nothing to flash asked for a tick")
			}
		})
	}
}

var background = regexp.MustCompile(`48;2;(\d+);(\d+);(\d+)`)

// tinted is the background of the line a key is drawn on, empty where it has none.
func tinted(l *tui.List, key string) string {
	for _, line := range strings.Split(l.ListFrame(), "\n") {
		if strings.Contains(plain(line), "repo#"+key) {
			return background.FindString(line)
		}
	}

	return ""
}

func brightness(t *testing.T, rgb string) int {
	t.Helper()

	sum := 0

	for _, n := range strings.Split(strings.TrimPrefix(rgb, "48;2;"), ";") {
		v, err := strconv.Atoi(n)
		if err != nil {
			t.Fatalf("reading the background %q: %v", rgb, err)
		}

		sum += v
	}

	return sum
}

// The tint is the accent mixed into the row's background, a little less each
// step until it is gone. The row under the cursor keeps the selection band
// whatever its step, so the flash never competes with it.
func TestTheTintFadesOverFourStepsAndStaysUnderTheBand(t *testing.T) {
	t.Parallel()

	l, _ := reordering(t, true, "A", "B", "C")
	l.Update(swapped{"A", "C", "B"})

	if got := l.Flash("C"); got != 4 {
		t.Fatalf("C is at step %d, want 4", got)
	}

	band := tinted(l, "A")
	if band == "" {
		t.Fatal("the row under the cursor has no band")
	}

	var seen []string

	for step := 4; step >= 1; step-- {
		tint := tinted(l, "C")
		if tint == "" || tint == band {
			t.Fatalf("step %d tint = %q, want a background of its own beside the band %q", step, tint, band)
		}

		seen = append(seen, tint)

		l.Update(tui.FlashTick(l.FlashGen()))
	}

	for i := 1; i < len(seen); i++ {
		step := brightness(t, seen[i-1]) - brightness(t, seen[i])
		overall := brightness(t, seen[0]) - brightness(t, seen[len(seen)-1])
		fading := step * overall
		if seen[i] == seen[i-1] || fading <= 0 {
			t.Errorf("the tint went %v, want each step a shade nearer the row's own background", seen)

			break
		}
	}

	if got := tinted(l, "C"); got != "" {
		t.Errorf("the settled row still has the background %q", got)
	}

	if got := tinted(l, "B"); got != "" {
		t.Errorf("a row that only slid has the background %q", got)
	}

	under, _ := reordering(t, true, "A", "B", "C")
	under.Update(swapped{"C", "A", "B"})

	if got := under.Flash("C"); got != 4 || under.CursorKey() != "C" {
		t.Fatalf("C is at step %d with the cursor on %q, want it flashing under the cursor", got, under.CursorKey())
	}

	if got := tinted(under, "C"); got != band {
		t.Errorf("the flashing row under the cursor is drawn on %q, want the band %q", got, band)
	}
}

func TestTheTintIsAMarkerWhereTheTerminalHasNoColor(t *testing.T) {
	t.Parallel()

	l, _ := reordering(t, true, "A", "B", "C")
	l.Update(tea.ColorProfileMsg{Profile: colorprofile.Ascii})
	l.Update(swapped{"A", "C", "B"})

	marked := func() []string {
		var out []string

		for _, line := range strings.Split(plain(l.ListFrame()), "\n") {
			if strings.Contains(line, tui.FlashMark) && strings.Contains(line, "repo#") {
				out = append(out, line[strings.Index(line, "repo#")+len("repo#"):][:1])
			}
		}

		return out
	}

	for range 4 {
		if got := marked(); !slices.Equal(got, []string{"C"}) {
			t.Fatalf("marked rows = %v, want only C while it flashes", got)
		}

		l.Update(tui.FlashTick(l.FlashGen()))
	}

	if got := marked(); len(got) != 0 {
		t.Errorf("marked rows = %v after the flash, want none", got)
	}
}
