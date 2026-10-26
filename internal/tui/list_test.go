package tui_test

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/kyleking/aragonite/tui/table"

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
				Key: "T1", Left: "kyleking/tlr#118", Repo: "kyleking/tlr", Mid: "internal/pool/pool.go:42",
				Age: "2h", Tail: "alice  2 replies  add TTL to the pool",
				Under: "good catch, pushed a defer", Unread: true,
				Detail: []string{"you:", "  does this leak on cancel?", "alice:", "  good catch, pushed a defer"},
			},
			{
				Key: "T2", Left: "kyleking/a-much-longer-repository-name#7",
				Repo: "kyleking/a-much-longer-repository-name", Mid: "review body",
				Age: "13h", Tail: "bob  fix the footnote reorder", Under: "two questions inline",
				Unread: true,
			},
		}},
		{Name: "waiting on you", Rows: nil},
		{Name: "awaiting others", Rows: []tui.Row{
			{
				Key: "T3", Left: "kyleking/wavez#7", Repo: "kyleking/wavez", Mid: "internal/lease/lease.go:9",
				Age: "4d", Tail: "KyleKing  scheduler leases", Under: "no, that one is per worker",
			},
		}},
	}
}

func list(t *testing.T, act tui.Act) *tui.List {
	t.Helper()

	l := tui.NewList("second-look conversations", queue, act).
		WithState(func() tui.State { return tui.State{Text: "sorted by triage"} })
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

// The row under the cursor is lit across the frame in the selection tint, its
// title and the meta line under it alike, and no other row carries a background.
func TestTheCursorRowIsLit(t *testing.T) {
	t.Parallel()

	for _, width := range []int{80, 200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			l := list(t, nil)
			l.Update(tea.WindowSizeMsg{Width: width, Height: 20})

			lit := map[int]bool{}

			for i, line := range strings.Split(l.ListFrame(), "\n") {
				lit[i] = strings.Contains(line, "\x1b[48;")

				if lit[i] && xansi.StringWidth(line) < width-1 {
					t.Errorf("line %d is lit for %d cells of %d", i, xansi.StringWidth(line), width-1)
				}
			}

			want := map[int]bool{3: true, 4: true}
			for i := range 8 {
				if lit[i] != want[i] {
					t.Errorf("line %d lit = %v, want %v", i, lit[i], want[i])
				}
			}
		})
	}
}

// A terminal with no color has no band to paint, so the row under the cursor is
// reversed across its width instead, and it is the only row that is.
func TestTheCursorRowIsReversedWhereThereIsNoColor(t *testing.T) {
	t.Parallel()

	const width = 120

	for _, tc := range []struct {
		name    string
		profile colorprofile.Profile
		mark    string
		absent  string
		height  int
	}{
		{"ascii", colorprofile.ASCII, "\x1b[7m", "\x1b[48;", 34},
		{"ascii beside a scrollbar", colorprofile.ASCII, "\x1b[7m", "\x1b[48;", 17},
		{"truecolor", colorprofile.TrueColor, "\x1b[48;", "\x1b[7m", 34},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns}}, 0)
			l.Update(tea.ColorProfileMsg{Profile: tc.profile})
			l.Update(tea.WindowSizeMsg{Width: width, Height: tc.height})
			l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

			var marked []string

			for _, line := range panelless(l.ListFrame()) {
				if strings.Contains(line, tc.absent) {
					t.Errorf("a line carries %q: %q", tc.absent, line)
				}

				if strings.Contains(line, tc.mark) {
					marked = append(marked, line)
				}
			}

			if len(marked) != 1 {
				t.Fatalf("%d rows carry %q, want exactly one", len(marked), tc.mark)
			}

			if got := xansi.StringWidth(marked[0]); got != width {
				t.Errorf("the marked row is %d cells wide, want %d", got, width)
			}
		})
	}
}

// The legend answers the wheel the way the review's does: it scrolls rather
// than closing, since a wheel tick is not a key, and it gives the mouse back
// when the box closes.
func TestListHelpScrollsOnTheWheel(t *testing.T) {
	t.Parallel()

	l := list(t, nil)
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	l.Update(tea.KeyPressMsg{Code: '?', Text: "?"})

	if mode := l.View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Fatalf("the legend did not capture the mouse: %v", mode)
	}

	if got := plain(l.ListFrame()); strings.Contains(got, "leave") {
		t.Fatalf("the legend's tail was already visible:\n%s", got)
	}

	for range 10 {
		l.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}

	if got := plain(l.ListFrame()); !strings.Contains(got, "leave") {
		t.Errorf("the wheel did not reach the legend's tail:\n%s", got)
	}

	l.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	if mode := l.View().MouseMode; mode != tea.MouseModeNone {
		t.Errorf("the closed legend still captures the mouse: %v", mode)
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

// A queue is triaged by jumping between what is still unread, so u lands on
// the next unread row whatever stands between and wraps around the end, and U
// does the same walking back.
func TestListJumpsBetweenUnreadRows(t *testing.T) {
	t.Parallel()

	l := list(t, nil)

	for key, want := range map[string][]string{
		"u": {"T2", "T1", "T2"},
		"U": {"T2", "T1", "T2"},
	} {
		for i, at := range want {
			code, _ := utf8.DecodeRuneInString(key)
			l.Update(tea.KeyPressMsg{Code: code, Text: key})

			if got := l.CursorKey(); got != at {
				t.Errorf("%s press %d landed on %q, want %q", key, i+1, got, at)
			}
		}

		l.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
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
	if !strings.Contains(lines[1], "pending your review") {
		t.Errorf("the header does not name the section the cursor is in:\n%s", lines[1])
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

	shows(t, l, []string{"pending your review", "searching…"}, []string{"repo#1", "repo#2"})

	// The later section answers first, which is what the cursor has to survive.
	l.Update(feed.order[0])
	shows(t, l, []string{"repo#2", "pending your review", "searching…"}, []string{"repo#1"})

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

	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	_, cmd := l.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q did not leave the screen while a filter was on")
	}

	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("q on a filter answered with something other than leaving")
	}

	if _, cmd := l.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Error("escape on a filter left the screen")
	}

	shows(t, l, []string{"kyleking/wavez#7"}, []string{"showing 1 of 3"})
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

// The three queues are one program with three tabs. Every tab is loaded when
// the screen opens, switching to one or back to it reads nothing again, the
// cursor is where it was left, and the strip says which one is being read.
func TestTabsLoadOnceAndKeepWhereEachWasLeft(t *testing.T) {
	t.Parallel()

	late := &counted{}

	l := tui.NewTabs([]tui.Tab{
		{
			Name:     "conversations",
			Sections: queue, Act: func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil },
		},
		{
			Name:     "inbox",
			Sections: func() []tui.Section { return late.rows },
			Act:      func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil },
			Loader:   late,
		},
	}, 0)
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	l.Init()

	if late.starts != 1 {
		t.Errorf("the tab nobody looked at ran %d search(es) on opening, want 1", late.starts)
	}

	l.Init()

	// Leave the cursor somewhere the other tab cannot be confused with.
	l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	shows(t, l, []string{"1 conversations", "2 inbox"}, nil)

	if got := l.CursorKey(); got != "T2" {
		t.Fatalf("the cursor sits on %q, want the second row", got)
	}

	l.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	shows(t, l, []string{"repo#9"}, []string{"kyleking/tlr#118"})

	if late.starts != 1 {
		t.Errorf("the tab ran %d search(es) after being opened, want 1", late.starts)
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

// A count read from a snapshot is the same text in a dimmer chip, on the tab
// being read and on one that is not.
func TestAStaleCountDrawsDimmerThanAFreshOne(t *testing.T) {
	t.Parallel()

	frame := func(stale bool, at int) string {
		l := tui.NewTabs([]tui.Tab{
			{
				Name: "inbox", Sections: lanes, Columns: laneColumns,
				Badge: func() tui.Badge { return tui.Badge{Label: "4", Stale: stale} },
			},
			{
				Name: "other", Sections: queue,
				Badge: func() tui.Badge { return tui.Badge{Label: "2", Stale: stale} },
			},
		}, at)
		l.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

		return l.ListFrame()
	}

	for at := range 2 {
		fresh, stale := frame(false, at), frame(true, at)

		if plain(fresh) != plain(stale) {
			t.Errorf("tab %d: the stale count reads differently from the fresh one", at+1)
		}

		if fresh == stale {
			t.Errorf("tab %d: the stale count is drawn the same as the fresh one", at+1)
		}
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

// The rating and the size each get a column of their own rather than being
// spliced onto the front of the title. A queue re-sorts as ratings land, so a
// title whose start moved with the numbers beside it jumped sideways under the
// eye on every answer.
func TestARatingIsAColumnRatherThanTitleText(t *testing.T) {
	t.Parallel()

	rated := func() []tui.Section {
		return []tui.Section{{Name: "pending your review", Rows: []tui.Row{
			{
				Key: "A", Left: "o/r#1", Mid: "alice", Age: "2h",
				Cost: "7", Added: "9", Removed: "2", Tail: "the title",
			},
			{Key: "B", Left: "o/r#2", Mid: "bob", Age: "4d", Tail: "the title"},
			{
				Key: "C", Left: "o/r#3", Mid: "carol", Age: "1d",
				Cost: "84", Added: "1.2k", Removed: "340", Tail: "the title",
			},
		}}}
	}

	l := tui.NewList("inbox", rated, nil)
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	at := startsOf(l.ListFrame(), "the title")

	if len(at) != 3 {
		t.Fatalf("found %d titles, want 3:\n%s", len(at), plain(l.ListFrame()))
	}

	if at[0] != at[1] || at[1] != at[2] {
		t.Errorf("titles start at columns %v, want one column for all three", at)
	}

	if !strings.Contains(plain(l.ListFrame()), "84") {
		t.Error("the rating is not drawn")
	}

	// The signs line up down the list, which two columns give and one figure
	// with a space in it does not.
	if got := startsOf(l.ListFrame(), "-"); len(got) != 2 || got[0] != got[1] {
		t.Errorf("the removed column starts at %v, want one column for both rows", got)
	}
}

// The forge's word and the agent's are columns of their own: a row that does
// not carry one leaves it blank rather than sliding the words after it left,
// and a frame with no room hides the longest column behind a count rather
// than cutting a word in half.
func TestTrailingWordsKeepTheirColumns(t *testing.T) {
	t.Parallel()

	rows := func() []tui.Section {
		return []tui.Section{{Name: "staged", Rows: []tui.Row{
			{
				Key: "a", Left: "kyleking/really-quite-long-repo-name#1", Mid: "blocked",
				Age: "7d", Tail: "2 drafts", Remote: "merged", Agent: "agent blocked",
			},
			{
				Key: "b", Left: "kyleking/really-quite-long-repo-name#2", Mid: "empty",
				Age: "29d", Tail: "no comments", Remote: "merged",
			},
		}}}
	}

	l := tui.NewList("staged", rows, nil)
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	frame := rowsOf(l.ListFrame())

	if at := startsOf(frame, "merged"); len(at) != 2 || at[0] != at[1] {
		t.Errorf("the forge's word lands at columns %v, want one column for both rows:\n%s", at, frame)
	}

	// Narrowing hides the tail first and says so; the words still moving keep
	// their columns.
	l.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	frame = rowsOf(l.ListFrame())

	for _, want := range []string{"…+1", "merged", "agent blocked"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the narrow frame is missing %q:\n%s", want, frame)
		}
	}

	if strings.Contains(frame, "drafts") || strings.Contains(frame, "comments") {
		t.Errorf("the hidden column's words still draw:\n%s", frame)
	}
}

// A list nothing rates spends no width on the column, the way the scrollbar is
// only drawn where the content overflows.
func TestAnUnratedListSpendsNoWidthOnTheColumn(t *testing.T) {
	t.Parallel()

	l := list(t, nil)
	l.Update(tea.WindowSizeMsg{Width: 140, Height: 20})

	bare := startsOf(l.ListFrame(), "add TTL to the pool")
	if len(bare) != 1 {
		t.Fatalf("the fixture row is drawn %d times, want once", len(bare))
	}

	withCost := tui.NewList("conversations", func() []tui.Section {
		rows := queue()
		rows[0].Rows[0].Cost = "9"

		return rows
	}, nil)
	withCost.Update(tea.WindowSizeMsg{Width: 140, Height: 20})

	got := startsOf(withCost.ListFrame(), "add TTL to the pool")
	if len(got) != 1 || got[0] != bare[0]+len("  9") {
		t.Errorf("the tail is at %v with a rating and %v without", got, bare)
	}
}

// rowsOf is a frame without the panel pinned under it, which repeats the row
// under the cursor and so is not what a test of the rows is asking about.
func rowsOf(frame string) string {
	return strings.Join(panelless(plain(frame)), "\n")
}

// panelless is a frame's lines above the panel, which keeps its own background
// in every profile.
func panelless(frame string) []string {
	const panelRows = 3

	lines := strings.Split(frame, "\n")

	return lines[:len(lines)-panelRows]
}

// startsOf is the column each line of the rows puts want at, measured in cells
// so a wide glyph is one column rather than the bytes it is written in.
func startsOf(frame, want string) []int {
	var out []int

	for _, line := range strings.Split(rowsOf(frame), "\n") {
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

// Opening a review closes the queue and builds it again afterwards, so without
// a Resume every return lands at the top rather than on the row that was opened.
func TestAQueueComesBackWhereTheReviewWasOpened(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		again func() []tui.Section
		want  string
	}{
		{name: "the row is still there", again: queue, want: "T3"},
		{
			// A review that posted while it was open leaves no row to return to.
			name:  "the row it was left on has gone",
			again: func() []tui.Section { return withoutRow(queue(), "T3") },
			want:  "T2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := tui.NewTabs(tabsOver(queue), 0)
			before.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			before.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})

			if got := before.CursorKey(); got != "T3" {
				t.Fatalf("the cursor sits on %q, want the last row", got)
			}

			after := tui.NewTabs(tabsOver(tc.again), 0)
			after.Restore(before.Where())
			after.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

			if got := after.CursorKey(); got != tc.want {
				t.Errorf("the queue came back on %q, want %q", got, tc.want)
			}
		})
	}
}

// A filter is part of where a tab was left.
func TestAQueueComesBackNarrowedTheWayItWasLeft(t *testing.T) {
	t.Parallel()

	before := tui.NewTabs(tabsOver(queue), 0)
	before.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	for _, r := range "/wavez" {
		before.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	before.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	shows(t, before, []string{"kyleking/wavez#7"}, []string{"kyleking/tlr#118"})

	after := tui.NewTabs(tabsOver(queue), 0)
	after.Restore(before.Where())
	after.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	shows(t, after, []string{"kyleking/wavez#7"}, []string{"kyleking/tlr#118"})
}

// tabsOver is the one tab both resume tests need on each side of the handoff.
func tabsOver(sections func() []tui.Section) []tui.Tab {
	return []tui.Tab{{
		Name: "staged", Sections: sections,
		Act: func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil },
	}}
}

func withoutRow(sections []tui.Section, key string) []tui.Section {
	for i := range sections {
		kept := make([]tui.Row, 0, len(sections[i].Rows))

		for j := range sections[i].Rows {
			if sections[i].Rows[j].Key != key {
				kept = append(kept, sections[i].Rows[j])
			}
		}

		sections[i].Rows = kept
	}

	return sections
}

// focusQueues are two tabs sharing a repository, and a row standing for a
// search that failed, which names none.
func focusQueues() []tui.Tab {
	act := func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil }

	inbox := func() []tui.Section {
		return []tui.Section{{Name: "waiting on you", Rows: []tui.Row{
			{Key: "kyleking/tlr#118", Left: "kyleking/tlr#118", Repo: "kyleking/tlr", Tail: "add a TTL"},
			{Key: "kyleking/wavez#7", Left: "kyleking/wavez#7", Repo: "kyleking/wavez", Tail: "leases"},
			{Left: "could not be read", Tail: "gh said no"},
		}}}
	}

	staged := func() []tui.Section {
		return []tui.Section{{Name: "staged", Rows: []tui.Row{
			{Key: "kyleking/tlr#118", Left: "kyleking/tlr#118", Repo: "kyleking/tlr", Tail: "2 comments"},
			{Key: "kyleking/other#3", Left: "kyleking/other#3", Repo: "kyleking/other", Tail: "1 comment"},
		}}}
	}

	return []tui.Tab{
		{Name: "inbox", Sections: inbox, Act: act},
		{Name: "staged", Sections: staged, Act: act},
	}
}

// A sitting is one repository at a time, so f narrows every tab rather than the
// one it was pressed on, and it survives the handoff that closes the screen to
// open a review. A filter does neither, which is why focus is not one.
func TestFocusNarrowsEveryQueueAndSurvivesAHandoff(t *testing.T) {
	t.Parallel()

	before := tui.NewTabs(focusQueues(), 0).
		WithFocusNote(func(repo string) tea.Cmd {
			return func() tea.Msg { return tui.FocusNote{Repo: repo, Note: "app-wt clean"} }
		})
	before.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	_, cmd := before.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if cmd == nil {
		t.Fatal("focusing asked the caller nothing about the repository")
	}

	before.Update(cmd())

	// An answer about a repository no longer in focus is drawn against none.
	before.Update(tui.FocusNote{Repo: "kyleking/wavez", Note: "somewhere else"})

	if got := before.Focused(); got != "kyleking/tlr" {
		t.Fatalf("focused %q, want kyleking/tlr", got)
	}

	// The failed search stays: the reason a section is short is the one thing
	// narrowing a queue must not hide.
	shows(t, before, []string{"kyleking/tlr#118", "app-wt clean", "could not be read"},
		[]string{"kyleking/wavez#7", "somewhere else"})

	before.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	shows(t, before, []string{"kyleking/tlr#118"}, []string{"kyleking/other#3"})

	after := tui.NewTabs(focusQueues(), 1)
	after.Restore(before.Where())
	after.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	shows(t, after, []string{"kyleking/tlr#118"}, []string{"kyleking/other#3"})

	after.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})

	if got := after.Focused(); got != "" {
		t.Errorf("F left the screen focused on %q", got)
	}

	shows(t, after, []string{"kyleking/other#3"}, nil)

	// A row with no repository behind it cannot be focused, and says so rather
	// than narrowing to nothing.
	after.Update(tea.KeyPressMsg{Code: '1', Text: "1"})

	for range 2 {
		after.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	after.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})

	if got := after.Focused(); got != "" {
		t.Errorf("a row naming no repository focused %q", got)
	}

	shows(t, after, []string{"belongs to no repository", "kyleking/wavez#7"}, nil)
}

// runMsgs executes the commands a keypress returns, the way the program loop
// does for a BatchMsg, feeding the messages they answer back into the list.
func runMsgs(l *tui.List, cmd tea.Cmd) {
	if cmd == nil {
		return
	}

	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runMsgs(l, c)
		}
	default:
		l.Update(msg)
	}
}

// Leaving a focused repository runs the hook hung on the list, which is how a
// claimed checkout is handed back, and the message it answers with lands in
// the footer. Focusing fires nothing: there is nothing to hand back yet.
func TestLeavingFocusRunsTheChangeHook(t *testing.T) {
	t.Parallel()

	var left []string

	l := tui.NewTabs(focusQueues(), 0).
		WithFocusChange(func(from, _ string) tea.Cmd {
			return func() tea.Msg {
				left = append(left, from)

				return tui.StatusMsg{Text: "released " + from}
			}
		})
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	_, cmd := l.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	runMsgs(l, cmd)

	if len(left) != 0 {
		t.Fatalf("focusing fired the hook for %v, want silence", left)
	}

	_, cmd = l.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
	runMsgs(l, cmd)

	if len(left) != 1 || left[0] != "kyleking/tlr" {
		t.Fatalf("the hook fired for %v, want kyleking/tlr leaving", left)
	}

	shows(t, l, []string{"released kyleking/tlr"}, nil)
}

// A conversation queue holds three kinds of row, and answering a review thread
// is different work from answering a comment on the pull request itself. The
// rows' own words cannot tell them apart: "review" appears in half of them.
func TestTheFilterKeepsOneKindOfRow(t *testing.T) {
	t.Parallel()

	sections := func() []tui.Section {
		return []tui.Section{{Name: "your conversations", Rows: []tui.Row{
			{Key: "a", Left: "kyleking/tlr#118", Kind: "thread", Tail: "alice  add a TTL to the pool"},
			{Key: "b", Left: "kyleking/tlr#118", Kind: "comment", Tail: "bob  the review body says"},
			{Key: "c", Left: "kyleking/wavez#7", Kind: "review", Tail: "carol  pool leases"},
		}}}
	}

	l := tui.NewList("second-look conversations", sections,
		func(tui.Action, *tui.Row) (string, bool, error) { return "", false, nil })
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	typeList(l, "/kind:thread")
	shows(t, l, []string{"kyleking/tlr#118", "showing 1 of 3"}, []string{"kyleking/wavez#7"})

	// A kind and a word narrow at once, and the kind is not matched as text.
	l.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	typeList(l, "/kind:comment pool")
	shows(t, l, []string{"showing 0 of 3"}, nil)

	l.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	typeList(l, "/kind:review pool")
	shows(t, l, []string{"kyleking/wavez#7", "showing 1 of 3"}, []string{"kyleking/tlr#118"})
}

// laneColumns is the inbox's grid, as the screen that owns it declares it.
func laneColumns() []table.Column {
	return []table.Column{
		{Key: "ref", Min: 8, Max: 8, Weight: 1, Trim: table.TrimLeft},
		{Key: "title", Min: 30, Weight: 3},
		{Key: "author", Min: 10, Max: 10, From: 112},
		{Key: "age", Min: 4, Max: 4, Align: table.AlignRight},
		{Key: "size", Min: 11, Max: 11, From: 100, Align: table.AlignRight},
		{Key: "status", Min: 16, Max: 26, Weight: 1},
	}
}

func sp(role tui.Role, text string) tui.Span { return tui.Span{Role: role, Text: text} }

func status(glyph string, tone tui.Role, short, long string) tui.Cell {
	words := func(word string) []tui.Span { return []tui.Span{sp(tone, glyph), sp(tui.RoleCell, " "+word)} }

	return tui.Cell{Spans: words(short), Long: words(long), LongAt: 24}
}

func size(added, removed string) tui.Cell {
	count := func(word string) string { return fmt.Sprintf("%5s", word) }

	return tui.Cell{Spans: []tui.Span{
		sp(tui.RoleAdd, count(added)), sp(tui.RoleCell, " "), sp(tui.RoleRemove, count(removed)),
	}}
}

// wordsOf is what the first set of spans that has any says as plain text.
func wordsOf(sets ...[]tui.Span) string {
	for _, set := range sets {
		var b strings.Builder

		for _, s := range set {
			b.WriteString(s.Text)
		}

		if b.Len() > 0 {
			return b.String()
		}
	}

	return ""
}

// gridRow is one inbox row on the grid, with the pieces the screen splits a
// title into: the mark, the commit prefix, and the subject.
func gridRow(key, owner, mark, prefix, subject, author, age string, size, status tui.Cell, dim bool) tui.Row {
	name := key[strings.Index(key, "/")+1:]
	ref := []tui.Span{sp(tui.RoleCell, name)}

	if owner != "" {
		ref = append([]tui.Span{sp(tui.RoleQuiet, owner)}, ref...)
	}

	lead := tui.RoleCell
	switch mark {
	case "!":
		lead = tui.RolePriority
	case "╭", "├", "╰":
		lead = tui.RoleQuiet
	}

	about := tui.About{
		Subject: subject, Prefix: strings.TrimSuffix(prefix, ": "), Ref: key,
		Facts: []tui.Fact{
			{Text: author},
			{Text: age + " old"},
			{Text: strings.TrimPrefix(wordsOf(status.Long, status.Spans), "·")},
			{Text: strings.Join(strings.Fields(strings.TrimPrefix(wordsOf(size.Spans), "·")), " "), Column: "size"},
		},
	}

	return tui.Row{
		Key: key, Dim: dim, About: &about,
		Cells: map[string]tui.Cell{
			"ref": {Spans: ref},
			"title": {Spans: []tui.Span{
				sp(lead, mark), sp(tui.RoleCell, " "), sp(tui.RoleQuiet, prefix), sp(tui.RoleText, subject),
			}},
			"author": {Spans: []tui.Span{sp(tui.RoleCell, author)}},
			"age":    {Spans: []tui.Span{sp(tui.RoleAge, age)}},
			"size":   size,
			"status": status,
		},
	}
}

// lanes carries every state the grid draws: the priority mark, a stack's connectors, a ref that
// needs its owner, a fact not yet known, a status in two lengths, a dim lane, and a folded one.
func lanes() []tui.Section {
	ready := status("✓", tui.RoleGood, "approved", "approved, checks passing")
	waits := status("○", tui.RoleMuted, "awaiting", "awaiting review")
	pending := tui.Pending()

	return []tui.Section{
		{Name: "ready to merge", Rows: []tui.Row{
			gridRow("o/a#1", "", " ", "feat(inbox): ", "order the queue by lane", "kyleking", "2d",
				size("+57", "-0"), ready, false),
		}},
		{Name: "new for you", Rows: []tui.Row{
			gridRow("o/b#2", "", "!", "fix: ", "a hotfix for the lease", "maria", "9d",
				pending, tui.Cell{Spans: []tui.Span{sp(tui.RoleCell, "priority")}}, false),
			gridRow("o/c#3", "o/", "╭", "chore: ", "bump a dependency whose name is long enough to be cut", "sam", "4d",
				pending, pending, false),
			gridRow("o/c#7", "o/", "╰", "feat: ", "use the bump", "sam", "4d", pending, tui.Cell{}, false),
			gridRow("p/c#8", "p/", " ", "", "no conventional prefix on this one", "lee", "3d", size("+1.2k", "-340"),
				tui.Cell{Spans: []tui.Span{sp(tui.RoleMuted, "◌"), sp(tui.RoleCell, " draft")}}, false),
		}},
		{Name: "waiting on others", Recedes: true, Rows: []tui.Row{
			gridRow("o/d#4", "", " ", "docs: ", "say why", "kyleking", "1d", size("+1", "-1"), waits, true),
		}},
		{Name: "merged", Fold: true, Recedes: true, Rows: []tui.Row{{Key: "o/e#5", Left: "o/e#5"}}},
	}
}

func TestLaneFrames(t *testing.T) {
	t.Parallel()

	for _, dims := range []struct{ width, height int }{
		{80, 20}, {100, 20}, {120, 20}, {160, 20}, {200, 20}, {120, 34}, {80, 24}, {80, 15},
	} {
		name := strconv.Itoa(dims.width)
		if dims.height != 20 {
			name += "x" + strconv.Itoa(dims.height)
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			l := tui.NewTabs([]tui.Tab{
				{
					Name: "inbox", Sections: lanes, Columns: laneColumns,
					Badge: func() tui.Badge { return tui.Badge{Label: "4"} },
					State: func() tui.State { return tui.State{Text: "sorted by triage"} },
					Hints: [][2]string{{"enter", "review"}, {"M", "merge"}},
					Inert: func(r *tui.Row) []string {
						if r != nil && r.Key == "o/a#1" {
							return nil
						}

						return []string{"M"}
					},
				},
				{
					Name: "other", Sections: queue,
					Badge: func() tui.Badge { return tui.Badge{Label: "2", Alert: true} },
				},
			}, 0)
			l.Update(tea.WindowSizeMsg{Width: dims.width, Height: dims.height})
			l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

			golden.RequireEqual(t, []byte(plain(l.ListFrame())))
		})
	}
}

// The panel is part of the frame, so the frame is as tall as the terminal
// whatever the cursor is on and however short the terminal is, and the row
// under the cursor is always one of those the body shows.
func TestTheFrameKeepsItsHeight(t *testing.T) {
	t.Parallel()

	for _, height := range []int{10, 15, 16, 24, 34, 60} {
		t.Run(strconv.Itoa(height), func(t *testing.T) {
			t.Parallel()

			l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns}}, 0)
			l.Update(tea.WindowSizeMsg{Width: 80, Height: height})

			for range 12 {
				frame := l.ListFrame()
				if got := strings.Count(frame, "\n") + 1; got != height {
					t.Fatalf("the frame is %d lines tall in a terminal of %d", got, height)
				}

				if !strings.Contains(frame, "\x1b[48;") {
					t.Fatal("the row under the cursor is not drawn")
				}

				l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
			}
		})
	}
}

// The footer sheds whole hints as the frame narrows and never cuts one mid-word.
// The keys a queue is for stay while anything else is drawn, and the ones that
// shed first go first.
func TestTheFooterShedsWholeHintsByPriority(t *testing.T) {
	t.Parallel()

	all := []string{
		"enter review", "C check out", "m comment", "A approve", "M merge", "l labels",
		"o GitHub", "s sort", "? help",
	}
	keep := []string{"enter review", "A approve", "M merge", "o GitHub", "? help"}
	pairs := make([][2]string, len(all))

	for i, h := range all {
		key, word, _ := strings.Cut(h, " ")
		pairs[i] = [2]string{key, word}
	}

	for width := 80; width <= 120; width += 3 {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns, Hints: pairs}}, 0)
			l.Update(tea.WindowSizeMsg{Width: width, Height: 24})

			lines := strings.Split(plain(l.ListFrame()), "\n")
			footer := strings.TrimRight(lines[len(lines)-1], " ")

			var drawn []string

			for _, h := range all {
				if strings.Contains(footer, h) {
					drawn = append(drawn, h)
				}
			}

			if want := " " + strings.Join(drawn, "   "); footer != want {
				t.Fatalf("the footer is %q, want whole hints only: %q", footer, want)
			}

			for _, h := range keep {
				if !slices.Contains(drawn, h) {
					t.Errorf("%q was shed at %d columns", h, width)
				}
			}
		})
	}
}

// A column the grid had no room for is said in the inspector, and only there.
func TestTheInspectorSaysWhatTheGridDropped(t *testing.T) {
	t.Parallel()

	for width, want := range map[int]bool{80: true, 100: false, 120: false} {
		l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns}}, 0)
		l.Update(tea.WindowSizeMsg{Width: width, Height: 34})

		lines := strings.Split(plain(l.ListFrame()), "\n")
		panel := strings.Join(lines[len(lines)-3:], "\n")

		if got := strings.Contains(panel, "+57 -0"); got != want {
			t.Errorf("at %d columns the inspector says the size = %v, want %v:\n%s", width, got, want, panel)
		}
	}
}

// The cursor takes a background and nothing else on the row moves: every column
// sits where it did with the cursor elsewhere.
func TestTheCursorLeavesTheGridWhereItWas(t *testing.T) {
	t.Parallel()

	for _, width := range []int{80, 100, 120, 160} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			frame := func(moves int) []string {
				l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns}}, 0)
				l.Update(tea.WindowSizeMsg{Width: width, Height: 34})

				for range moves {
					l.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
				}

				return panelless(plain(l.ListFrame()))
			}

			lit := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns}}, 0)
			lit.Update(tea.WindowSizeMsg{Width: width, Height: 34})
			lit.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

			if bare := unlitRunes(lit.ListFrame()); bare != "" {
				t.Errorf("the lit row has cells with no background: %q", bare)
			}

			first, second := frame(0), frame(2)
			for i := range first {
				if strings.TrimRight(first[i], " ") != strings.TrimRight(second[i], " ") {
					t.Errorf("line %d moved with the cursor:\n%q\n%q", i, first[i], second[i])
				}
			}
		})
	}
}

// unlitRunes is the text of the first lit line that is drawn with no background,
// which is a reset inside the row that ended the band early.
func unlitRunes(frame string) string {
	for _, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, "\x1b[48;") {
			continue
		}

		var bare strings.Builder

		lit := false

		for rest := line; rest != ""; {
			if strings.HasPrefix(rest, "\x1b[") {
				end := strings.IndexByte(rest, 'm')
				params := rest[2:end]
				lit = (lit && params != "" && params != "0") || strings.Contains(params, "48;")
				rest = rest[end+1:]

				continue
			}

			r, n := utf8.DecodeRuneInString(rest)
			if !lit {
				bare.WriteRune(r)
			}

			rest = rest[n:]
		}

		return bare.String()
	}

	return ""
}

// An expanded row on the grid shows its conversation under it, and the row
// itself is drawn once rather than again for each line of the conversation.
func TestAnExpandedGridRowShowsItsConversationOnce(t *testing.T) {
	t.Parallel()

	withDetail := func() []tui.Section {
		out := lanes()
		out[0].Rows[0].Detail = []string{"alice:", "  does this leak on cancel?"}

		return out
	}

	l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: withDetail, Columns: laneColumns}}, 0)
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	frame := plain(l.ListFrame())
	if !strings.Contains(frame, "does this leak on cancel?") {
		t.Errorf("the conversation is not shown:\n%s", frame)
	}

	if got := strings.Count(frame, "order the queue by lane"); got != 2 {
		t.Errorf("the row is drawn %d times, want once in the body and once in the inspector:\n%s", got, frame)
	}
}

// A filter finds a grid row by what its cells and its inspector say, including
// a column the frame had no room for.
func TestAWordFiltersGridRowsByTheirCellsAndInspector(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		query string
		want  []string
		gone  []string
	}{
		{"/lease", []string{"a hotfix for the lease"}, []string{"say why"}},
		{"/lee", []string{"no conventional prefix"}, []string{"a hotfix for the lease"}},
		{"/draft", []string{"no conventional prefix"}, []string{"say why"}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()

			l := tui.NewTabs([]tui.Tab{{Name: "inbox", Sections: lanes, Columns: laneColumns}}, 0)
			l.Update(tea.WindowSizeMsg{Width: 80, Height: 34})
			typeList(l, tc.query)

			shows(t, l, tc.want, tc.gone)
		})
	}
}
