package tui_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/tui"
)

// errGHSaidNo stands in for an action that failed, which belongs in the footer
// rather than taking the screen down.
var errGHSaidNo = errors.New("gh said no")

// queue is a list in every state it can draw: an unread row, a read one, a row
// that expands, and an empty bucket, whose heading still prints because
// "nothing is waiting on you" is the answer most worth seeing.
func queue() []tui.Section {
	return []tui.Section{
		{Name: "new since you looked", Rows: []tui.Row{
			{
				Key: "T1", Left: "kyleking/tlr#118", Mid: "internal/pool/pool.go:42",
				Age: "2h", Tail: "alice  2 replies  add TTL to the pool",
				Under: "good catch, pushed a defer", Unread: true,
				Detail: []string{"you:", "  does this leak on cancel?", "alice:", "  good catch, pushed a defer"},
			},
			{
				Key: "T2", Left: "kyleking/a-much-longer-repository-name#7", Mid: "review body",
				Age: "13h", Tail: "bob  fix the footnote reorder", Under: "two questions inline",
				Unread: true,
			},
		}},
		{Name: "waiting on you", Rows: nil},
		{Name: "awaiting others", Rows: []tui.Row{
			{
				Key: "T3", Left: "kyleking/wavez#7", Mid: "internal/lease/lease.go:9",
				Age: "4d", Tail: "KyleKing  scheduler leases", Under: "no, that one is per worker",
			},
		}},
	}
}

func list(t *testing.T, act tui.Act) *tui.List {
	t.Helper()

	l := tui.NewList("second-look conversations", queue, act).
		WithSubtitle(func() string { return "3 conversations · 2 unread" })
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	return l
}

func TestListFrames(t *testing.T) {
	t.Parallel()

	frames := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"queue", nil},
		{"expanded", []tea.KeyPressMsg{{Code: tea.KeyEnter}}},
		{"help", []tea.KeyPressMsg{{Code: '?', Text: "?"}}},
	}

	for _, width := range []int{80, 120} {
		for _, frame := range frames {
			t.Run(fmt.Sprintf("%s/%d", frame.name, width), func(t *testing.T) {
				t.Parallel()

				l := list(t, nil)
				l.Update(tea.WindowSizeMsg{Width: width, Height: 20})

				for _, k := range frame.keys {
					l.Update(k)
				}

				golden.RequireEqual(t, []byte(plain(l.ListFrame())))
			})
		}
	}
}

// The cursor moves a row at a time, skipping headings, the quoted line under a
// row, and the lines an expanded row adds. Otherwise one press would sometimes
// move one row and sometimes half of one.
func TestListMovesARowAtATime(t *testing.T) {
	t.Parallel()

	l := list(t, nil)

	if got := l.CursorKey(); got != "T1" {
		t.Fatalf("the cursor opens on %q, want the first row", got)
	}

	for _, want := range []string{"T2", "T3", "T3"} {
		l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

		if got := l.CursorKey(); got != want {
			t.Errorf("j landed on %q, want %q", got, want)
		}
	}

	for _, want := range []string{"T2", "T1", "T1"} {
		l.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})

		if got := l.CursorKey(); got != want {
			t.Errorf("k landed on %q, want %q", got, want)
		}
	}
}

// Expanding a row must not move the cursor off it, which is what happens if the
// lines an expansion adds are counted as rows.
func TestListKeepsTheCursorWhenARowExpands(t *testing.T) {
	t.Parallel()

	l := list(t, nil)

	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := l.CursorKey(); got != "T1" {
		t.Errorf("expanding moved the cursor to %q", got)
	}

	if !strings.Contains(plain(l.ListFrame()), "does this leak on cancel?") {
		t.Error("the expanded row does not show the conversation")
	}

	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if strings.Contains(plain(l.ListFrame()), "does this leak on cancel?") {
		t.Error("enter again did not collapse the row")
	}
}

// A key pressed on a row reaches the caller with that row, and a failure lands
// in the footer rather than taking the screen down.
func TestListReportsWhatAnActionSaid(t *testing.T) {
	t.Parallel()

	var got []string

	act := func(a tui.Action, r *tui.Row) (string, bool, error) {
		got = append(got, fmt.Sprintf("%d:%s", a, r.Key))

		if a == tui.ActResolve {
			return "", false, errGHSaidNo
		}

		return "marked " + r.Key, false, nil
	}

	l := list(t, act)

	l.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

	if frame := plain(l.ListFrame()); !strings.Contains(frame, "marked T1") {
		t.Errorf("the footer does not carry what the action said:\n%s", frame)
	}

	l.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})

	if frame := plain(l.ListFrame()); !strings.Contains(frame, "gh said no") {
		t.Errorf("a failed action does not reach the footer:\n%s", frame)
	}

	want := fmt.Sprintf("%d:T1", tui.ActMark) + " " + fmt.Sprintf("%d:T1", tui.ActResolve)
	if strings.Join(got, " ") != want {
		t.Errorf("the caller saw %v, want %q", got, want)
	}
}

// enter on a row the caller says it is done with leaves the screen and reports
// which row it was, which is how choosing a staged review hands control back.
func TestListLeavesOnAChoice(t *testing.T) {
	t.Parallel()

	rows := func() []tui.Section {
		return []tui.Section{{Name: "staged", Rows: []tui.Row{{Key: "42", Left: "o/r#42", Mid: "ready"}}}}
	}

	l := tui.NewList("staged", rows, func(_ tui.Action, _ *tui.Row) (string, bool, error) {
		return "opening #42", true, nil
	})
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := l.Chosen(); got != "42" {
		t.Errorf("the screen was left on %q, want 42", got)
	}
}

// A frame narrower than the minimum says so rather than wrapping the rows into
// illegibility.
func TestListRefusesATinyTerminal(t *testing.T) {
	t.Parallel()

	l := list(t, nil)
	l.Update(tea.WindowSizeMsg{Width: 40, Height: 8})

	if !strings.Contains(l.ListFrame(), "too small") {
		t.Errorf("a tiny frame drew rows anyway:\n%s", l.ListFrame())
	}
}

// The heading a row sits under is the one thing a bucket is for, and it scrolls
// away exactly when the list is long enough to need it, so the header carries it
// from then on and not before.
func TestTheHeaderCarriesTheSectionOnceItScrollsOff(t *testing.T) {
	t.Parallel()

	long := func() []tui.Section {
		rows := make([]tui.Row, 0, 40)
		for i := range 40 {
			rows = append(rows, tui.Row{
				Key: fmt.Sprintf("T%d", i), Left: fmt.Sprintf("kyleking/tlr#%d", i), Age: "2h",
			})
		}

		return []tui.Section{{Name: "pending your review", Rows: rows}}
	}

	l := tui.NewList("second-look inbox", long, func(tui.Action, *tui.Row) (string, bool, error) {
		return "", false, nil
	})
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	if got := plain(l.ListFrame()); strings.Count(got, "pending your review") != 1 {
		t.Fatalf("the visible heading is named twice:\n%s", got)
	}

	for range 30 {
		l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	lines := strings.Split(plain(l.ListFrame()), "\n")
	if !strings.Contains(lines[0], "pending your review") {
		t.Errorf("the header does not name the section the cursor is in:\n%s", lines[0])
	}
}

// A configured inbox runs to eighty rows across four sections, which no layout
// makes scannable. The row you want is nearly always named by a word you know.
func TestTheFilterNarrowsAQueueAndPutsItBack(t *testing.T) {
	t.Parallel()

	l := list(t, func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil })

	typeList(l, "/pool")
	shows(t, l, []string{"kyleking/tlr#118", "showing 1 of 3"},
		[]string{"kyleking/wavez#7", "awaiting others"})

	l.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	shows(t, l, []string{"kyleking/wavez#7", "awaiting others"}, nil)

	// An author narrows it as readily as a title, and the heading the match sits
	// under survives so the row is not left standing on its own.
	typeList(l, "/bob")
	shows(t, l, []string{"a-much-longer-repository-name#7", "new since you looked"},
		[]string{"kyleking/tlr#118"})

	// Escape from the prompt, rather than from a committed filter, does the same.
	l.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	shows(t, l, []string{"kyleking/tlr#118"}, nil)
}

func typeList(l *tui.List, keys string) {
	for _, r := range keys {
		l.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func shows(t *testing.T, l *tui.List, want, gone []string) {
	t.Helper()

	frame := plain(l.ListFrame())

	for _, w := range want {
		if !strings.Contains(frame, w) {
			t.Errorf("%q is missing:\n%s", w, frame)
		}
	}

	for _, g := range gone {
		if strings.Contains(frame, g) {
			t.Errorf("%q survived the filter:\n%s", g, frame)
		}
	}
}

// landed is one section arriving, which is how a queue that runs its searches
// at once reaches the screen.
type landed struct {
	at      int
	section tui.Section
}

// dribble hands a list its sections one at a time, the way four independent
// searches answer: in whatever order they finish.
type dribble struct {
	shown []tui.Section
	order []landed
}

func (d *dribble) Start() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(d.order))

	for i := range d.order {
		next := d.order[i]
		cmds = append(cmds, func() tea.Msg { return next })
	}

	return tea.Batch(cmds...)
}

func (d *dribble) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	got, ok := msg.(landed)
	if !ok {
		return nil, false
	}

	d.shown[got.at] = got.section

	return nil, true
}

// A queue that runs four independent searches has no reason to show nothing
// until the slowest answers. Each section is drawn as it lands, the headings
// say which are still out, and the cursor stays at the top until it is moved,
// since a row arriving above it should not leave it in the middle of the list.
func TestAQueueDrawsEachSectionAsItLands(t *testing.T) {
	t.Parallel()

	feed := &dribble{
		shown: []tui.Section{
			{Name: "pending your review", Note: "searching…"},
			{Name: "reviewed, still open", Note: "searching…"},
		},
		order: []landed{
			{1, tui.Section{Name: "reviewed, still open", Rows: []tui.Row{{Key: "B", Left: "repo#2"}}}},
			{0, tui.Section{Name: "pending your review", Rows: []tui.Row{{Key: "A", Left: "repo#1"}}}},
		},
	}

	l := tui.NewList("second-look inbox", func() []tui.Section { return feed.shown },
		func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil }).
		WithLoader(feed)
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	shows(t, l, []string{"pending your review (searching…)"}, []string{"repo#1", "repo#2"})

	// The later section answers first, which is what the cursor has to survive.
	l.Update(feed.order[0])
	shows(t, l, []string{"repo#2", "pending your review (searching…)"}, []string{"repo#1"})

	l.Update(feed.order[1])
	shows(t, l, []string{"repo#1", "repo#2"}, []string{"searching…"})

	if got := l.CursorKey(); got != "A" {
		t.Errorf("the cursor sits on %q, want the first row of the queue", got)
	}
}

// q means what it says, and a filter is a state esc gets out of. They shared one
// binding, so q cleared the filter instead of leaving and a pattern carrying a q
// canceled itself as it was typed.
func TestQuitLeavesAndEscapeClearsTheFilter(t *testing.T) {
	t.Parallel()

	l := list(t, func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil })

	// The pattern carries a q, which used to cancel the prompt as it was typed.
	typeList(l, "/questions")
	shows(t, l, []string{"showing 1 of 3", "a-much-longer-repository-name#7"}, nil)

	_, cmd := l.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Error("q did not leave the screen while a filter was on")
	}
}

// counted is a loader that records how many times it was started, since a tab
// nobody has looked at must cost no requests and a tab looked at twice must not
// pay for it twice.
type counted struct {
	starts int
	rows   []tui.Section
}

func (c *counted) Start() tea.Cmd {
	c.starts++
	c.rows = []tui.Section{{Name: "answered", Rows: []tui.Row{{Key: "X", Left: "repo#9"}}}}

	return nil
}

func (*counted) Absorb(tea.Msg) (tea.Cmd, bool) { return nil, false }

// The three queues are one program with three tabs. A tab is loaded when it is
// first looked at, switching back puts the cursor where it was left, and the
// strip says which one is being read.
func TestTabsLoadOnceAndKeepWhereEachWasLeft(t *testing.T) {
	t.Parallel()

	late := &counted{}

	l := tui.NewTabs([]tui.Tab{
		{
			Name: "conversations", Title: "second-look conversations",
			Sections: queue, Act: func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil },
		},
		{
			Name: "inbox", Title: "second-look inbox",
			Sections: func() []tui.Section { return late.rows },
			Act:      func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil },
			Loader:   late,
		},
	}, 0)
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	if late.starts != 0 {
		t.Errorf("the tab nobody looked at ran %d search(es)", late.starts)
	}

	// Leave the cursor somewhere the other tab cannot be confused with.
	l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	shows(t, l, []string{"[1] conversations", "[2] inbox"}, nil)

	if got := l.CursorKey(); got != "T2" {
		t.Fatalf("the cursor sits on %q, want the second row", got)
	}

	l.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	shows(t, l, []string{"repo#9"}, []string{"kyleking/tlr#118"})

	if late.starts != 1 {
		t.Errorf("the tab ran %d search(es) on being opened, want 1", late.starts)
	}

	l.Update(tea.KeyPressMsg{Code: '1', Text: "1"})

	if got := l.CursorKey(); got != "T2" {
		t.Errorf("the tab came back with the cursor on %q, want where it was left", got)
	}

	// Switching back is not a reason to search again.
	l.Update(tea.KeyPressMsg{Code: ']', Text: "]"})

	if late.starts != 1 {
		t.Errorf("the tab ran %d search(es) in total, want 1", late.starts)
	}
}

// A key that acts on a row must leave the cursor on it. Every action rebuilds
// the list, and a rebuild puts the cursor back at the top unless it knows the
// reader moved it, so `d` on the third staged review used to arm the first one:
// getting back to where you were was `djjd`.
func TestAnActionKeepsTheCursorWhereTheReaderPutIt(t *testing.T) {
	t.Parallel()

	l := list(t, func(_ tui.Action, r *tui.Row) (string, bool, error) {
		return "armed " + r.Key, false, nil
	})

	for range 2 {
		l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	for _, k := range []tea.KeyPressMsg{
		{Code: 'd', Text: "d"}, {Code: 'o', Text: "o"}, {Code: tea.KeySpace, Text: " "},
	} {
		l.Update(k)

		if got := l.CursorKey(); got != "T3" {
			t.Errorf("%q sent the cursor to %q, want it left on T3", k.Text, got)
		}
	}
}

// The rating gets a column of its own rather than being spliced onto the front
// of the title. A queue re-sorts as ratings land, so a title whose start moved
// with the number beside it jumped sideways under the eye on every answer.
func TestARatingIsAColumnRatherThanTitleText(t *testing.T) {
	t.Parallel()

	rated := func() []tui.Section {
		return []tui.Section{{Name: "pending your review", Rows: []tui.Row{
			{Key: "A", Left: "o/r#1", Mid: "alice", Age: "2h", Cost: "7", Tail: "the title"},
			{Key: "B", Left: "o/r#2", Mid: "bob", Age: "4d", Tail: "the title"},
			{Key: "C", Left: "o/r#3", Mid: "carol", Age: "1d", Cost: "84", Tail: "the title"},
		}}}
	}

	l := tui.NewList("inbox", rated, nil)
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	at := startsOf(plain(l.ListFrame()), "the title")

	if len(at) != 3 {
		t.Fatalf("found %d titles, want 3:\n%s", len(at), plain(l.ListFrame()))
	}

	if at[0] != at[1] || at[1] != at[2] {
		t.Errorf("titles start at columns %v, want one column for all three", at)
	}

	if !strings.Contains(plain(l.ListFrame()), "84") {
		t.Error("the rating is not drawn")
	}
}

// A list nothing rates spends no width on the column, the way the scrollbar is
// only drawn where the content overflows.
func TestAnUnratedListSpendsNoWidthOnTheColumn(t *testing.T) {
	t.Parallel()

	l := list(t, nil)

	bare := startsOf(plain(l.ListFrame()), "add TTL to the pool")
	if len(bare) != 1 {
		t.Fatalf("the fixture row is drawn %d times, want once", len(bare))
	}

	withCost := tui.NewList("conversations", func() []tui.Section {
		rows := queue()
		rows[0].Rows[0].Cost = "9"

		return rows
	}, nil)
	withCost.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	got := startsOf(plain(withCost.ListFrame()), "add TTL to the pool")
	if len(got) != 1 || got[0] != bare[0]+len("  9") {
		t.Errorf("the tail is at %v with a rating and %v without", got, bare)
	}
}

// startsOf is the column each line of a frame puts want at, measured in cells
// so the cursor glyph is one column rather than the three bytes it is written
// in.
func startsOf(frame, want string) []int {
	var out []int

	for _, line := range strings.Split(frame, "\n") {
		if i := strings.Index(line, want); i >= 0 {
			out = append(out, utf8.RuneCountInString(line[:i]))
		}
	}

	return out
}

// Ordering a queue by what a diff costs to read means a read per row, which on
// a configured inbox is eighty nobody asked for. So a row nothing has rated is
// paid for once the cursor has stopped on it, and running the cursor past one
// asks for nothing.
func TestTheCursorStoppingPaysForTheRowUnderIt(t *testing.T) {
	t.Parallel()

	asked := []string{}

	l := tui.NewList("second-look conversations", queue, nil).
		WithRest(func(key string) tea.Cmd {
			asked = append(asked, key)

			return nil
		})
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	stale := l.Armed()

	l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	// The timer the first move armed answers about a row the cursor has left,
	// so it is dropped rather than paid for.
	l.Settle(stale)

	if len(asked) != 0 {
		t.Fatalf("a row the cursor moved off was paid for: %v", asked)
	}

	l.Settle(l.Armed())

	if len(asked) != 1 || asked[0] != l.CursorKey() {
		t.Errorf("asked for %v, want the row under the cursor (%q)", asked, l.CursorKey())
	}
}
