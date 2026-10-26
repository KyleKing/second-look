package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/labels"
)

// several is the least number of tabs worth a strip across the top and a key to
// switch with: one queue is a screen, not a set of them.
const several = 2

// Tab is one queue in a list screen. The three second-look has -- the review
// queue, the conversations, and what is staged here -- are the same shape and
// are read in one sitting, so they are one program with three tabs rather than
// three programs that cannot see each other.
type Tab struct {
	// Name is what the strip calls it.
	Name     string
	Sections func() []Section
	Act      Act
	// State is what changes how to read the list, drawn in the header's
	// right-hand corner: the sort, or what is still loading.
	State func() State
	Hints [][2]string
	// Inert names the hinted keys that do nothing at a row, which the footer
	// draws dim. The row is nil where the cursor is on no row.
	Inert func(r *Row) []string
	Help  [][2]string
	// Badge is the count chip beside the tab's name in the strip, so a queue
	// says how much it holds without being switched to. An empty Label draws
	// nothing, which is what an unstarted or empty queue says.
	Badge func() Badge
	// Columns lays the tab's rows out on a grid, one line each, from each row's
	// Cells. It is a function because a column's width can follow the rows,
	// and it is read on every frame. A column's From is the terminal's width.
	Columns func() []table.Column
	// Loader fills the tab in as its answers arrive. Every tab's is started
	// when the screen opens, so each count is in the strip before its tab is
	// looked at.
	Loader Loader
	// Rest is called with the row the cursor has stopped on, for a tab that
	// pays for something only the row being looked at needs.
	Rest func(row string) tea.Cmd
	// Labeled says which pull request a row is and what it carries, and Relabel
	// records a label put on or taken off it. A tab with neither has no labels to pick.
	Labeled func(r *Row) (labels.Target, []string, bool)
	Relabel func(row, name string, on bool)
}

// Badge is a tab's count chip.
type Badge struct {
	Label string
	// Alert draws the count in red, for a queue holding something blocked.
	Alert bool
	// Stale draws the count dim: it was read from a snapshot and its refresh
	// has not landed, so it is not yet a fact.
	Stale bool
}

// State is the header's right-hand text. Busy draws it in the warning color,
// since a queue still loading reads differently from one that is settled.
type State struct {
	Text string
	Busy bool
}

// programName leads the header line of a screen that has tabs.
const programName = "second-look"

// view is where a tab was left. Switching back to one puts the cursor, the
// scroll, and the filter back where they were, since a tab that forgot them
// would be a tab nobody switches away from.
type view struct {
	cursor   int
	offset   int
	touched  bool
	expanded map[string]bool
	filter   filter
	status   string
	failed   bool
	started  bool
	armed    bool
}

// NewTabs builds a list screen with more than one queue in it, opening on the
// tab at index at.
func NewTabs(tabs []Tab, at int) *List {
	l := &List{
		title: programName,
		tabs:  tabs, at: min(max(at, 0), len(tabs)-1),
		views: make([]view, len(tabs)),
		keys:  defaultKeyMap(), list: defaultListKeys(), styles: loadStyles(),
		width: minWidth, height: startHeight, expanded: map[string]bool{}, filter: newFilter(),
	}

	for i := range l.views {
		l.views[i] = view{expanded: map[string]bool{}, filter: newFilter()}
	}

	l.adopt()
	l.rebuild()

	return l
}

// Tab is which queue was being read when the screen closed, so reopening it
// comes back to the same one.
func (l *List) Tab() int { return l.at }

// adopt points the screen at the current tab. The fields it copies are the ones
// the renderer and the key handler read, so everything below them stays unaware
// that there is more than one queue.
func (l *List) adopt() {
	if len(l.tabs) == 0 {
		return
	}

	t := &l.tabs[l.at]
	l.sections, l.act = t.Sections, t.Act
	l.state, l.hints, l.helpLines, l.inert = t.State, t.Hints, t.Help, t.Inert
	l.loader, l.onRest, l.columns = t.Loader, t.Rest, t.Columns
	l.labeled, l.relabel = t.Labeled, t.Relabel

	v := &l.views[l.at]
	l.cursor, l.offset, l.touched, l.armed = v.cursor, v.offset, v.touched, v.armed
	l.expanded, l.filter = v.expanded, v.filter
	l.status, l.failed = v.status, v.failed
}

// leftAt is where the tab being switched to was left, read before the rebuild
// that replaces the lines it indexes.
func (l *List) leftAt() view { return l.views[l.at] }

// remember writes back what the tab being left was showing.
func (l *List) remember() {
	if len(l.tabs) == 0 {
		return
	}

	v := &l.views[l.at]
	v.cursor, v.offset, v.touched, v.armed = l.cursor, l.offset, l.touched, l.armed
	v.expanded, v.filter = l.expanded, l.filter
	v.status, v.failed = l.status, l.failed
}

// StartAll starts every tab's loader that has not been started, in tab order,
// so the strip carries every count from the first frame on. A tab is started at
// most once: switching to it later does not read it again.
func (l *List) StartAll() tea.Cmd {
	var cmds []tea.Cmd

	for i := range l.tabs {
		if l.tabs[i].Loader == nil || l.views[i].started {
			continue
		}

		l.views[i].started = true
		cmds = append(cmds, l.tabs[i].Loader.Start())
	}

	return tea.Batch(cmds...)
}

// switchTo moves to a tab, starting its loader if nothing has yet.
func (l *List) switchTo(at int) tea.Cmd {
	if at < 0 || at >= len(l.tabs) || at == l.at {
		return nil
	}

	l.remember()
	l.at = at
	l.adopt()
	l.flash, l.flashGen = nil, l.flashGen+1

	v := l.leftAt()

	// The loader runs before the rebuild rather than after it: Start is what
	// puts the headings up and says the search is out, and a tab drawn ahead of
	// it reads as a queue with nothing in it.
	var cmd tea.Cmd

	if l.loader != nil && !l.views[at].started {
		l.views[at].started = true
		cmd = l.loader.Start()
	}

	// The rebuild has the tab being left still in l.lines, so it is given
	// nothing to match against and the cursor is put back afterwards by index:
	// the row a tab was left on is the row it comes back to.
	l.lines = nil
	l.rebuild()
	l.to(v.cursor)
	l.offset = min(v.offset, max(0, len(l.lines)-l.visible()))

	return cmd
}

// tabKey answers the keys that switch tabs: a digit picks one and ] and [ step
// through them, which is where a reader coming from a browser or from gh-dash
// reaches first.
func (l *List) tabKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if len(l.tabs) < several {
		return nil, false
	}

	switch {
	case key.Matches(msg, l.keys.Forward):
		return l.switchTo((l.at + 1) % len(l.tabs)), true
	case key.Matches(msg, l.keys.Backward):
		return l.switchTo((l.at - 1 + len(l.tabs)) % len(l.tabs)), true
	}

	if n, err := strconv.Atoi(msg.String()); err == nil && n >= 1 && n <= len(l.tabs) {
		return l.switchTo(n - 1), true
	}

	return nil, false
}

// stretch is the columns of a line a thing covers.
type stretch struct{ from, to int }

// tabGap separates one tab from the next.
const tabGap = "   "

// tabStrip is the tabs on the header line: each a digit, a name, and a count
// chip. It also says which cells the active tab spans, so the rule below can
// underline it.
func (l *List) tabStrip() (string, stretch) {
	if len(l.tabs) < several {
		return "", stretch{}
	}

	parts := make([]string, 0, len(l.tabs))
	col := 0
	var active stretch

	for i := range l.tabs {
		on := i == l.at
		name := l.styles.rungs.cell.dim
		chip := l.styles.badge

		if on {
			name = l.styles.rungs.title.dim
			chip = l.styles.badgeOn
		}

		part := name.Render(l.tabs[i].Name)

		if l.tabs[i].Badge != nil {
			if b := l.tabs[i].Badge(); b.Label != "" {
				switch {
				case b.Stale && on:
					chip = l.styles.badgeOnStale
				case b.Stale:
					chip = l.styles.badgeStale
				case b.Alert && !on:
					chip = l.styles.badgeBlocked
				}

				part += chip.Render(b.Label)
			}
		}

		if on {
			active = stretch{from: col, to: col + textWidth(part)}
		}

		col += textWidth(part) + len(tabGap)

		parts = append(parts, part)
	}

	return strings.Join(parts, tabGap), active
}
