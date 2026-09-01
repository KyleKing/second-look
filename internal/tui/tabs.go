package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// several is the least number of tabs worth a strip across the top and a key to
// switch with: one queue is a screen, not a set of them.
const several = 2

// Tab is one queue in a list screen. The three second-look has -- the review
// queue, the conversations, and what is staged here -- are the same shape and
// are read in one sitting, so they are one program with three tabs rather than
// three programs that cannot see each other.
type Tab struct {
	// Name is what the strip calls it, and Title what the header says.
	Name     string
	Title    string
	Sections func() []Section
	Act      Act
	Subtitle func() string
	Hints    [][2]string
	Help     [][2]string
	// Loader fills the tab in as its answers arrive. It is started when the tab
	// is first looked at rather than when the screen opens, so a queue nobody
	// switched to costs no requests.
	Loader Loader
}

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
}

// NewTabs builds a list screen with more than one queue in it, opening on the
// tab at index at.
func NewTabs(tabs []Tab, at int) *List {
	l := &List{
		tabs: tabs, at: min(max(at, 0), len(tabs)-1),
		views: make([]view, len(tabs)),
		keys:  defaultKeyMap(), list: defaultListKeys(), styles: newStyles(),
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
	l.title, l.sections, l.act = t.Title, t.Sections, t.Act
	l.subtitle, l.hints, l.helpLines = t.Subtitle, t.Hints, t.Help
	l.loader = t.Loader

	v := &l.views[l.at]
	l.cursor, l.offset, l.touched = v.cursor, v.offset, v.touched
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
	v.cursor, v.offset, v.touched = l.cursor, l.offset, l.touched
	v.expanded, v.filter = l.expanded, l.filter
	v.status, v.failed = l.status, l.failed
}

// switchTo moves to a tab, starting its loader the first time it is looked at.
func (l *List) switchTo(at int) tea.Cmd {
	if at < 0 || at >= len(l.tabs) || at == l.at {
		return nil
	}

	l.remember()
	l.at = at
	l.adopt()

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

// tabStrip is the row of names across the top. The key is bracketed inside the
// word the way every other hint in these tools is, and the tab being read is
// the one drawn in the title's own face.
func (l *List) tabStrip() string {
	if len(l.tabs) < several {
		return ""
	}

	parts := make([]string, 0, len(l.tabs))

	for i := range l.tabs {
		label := "[" + strconv.Itoa(i+1) + "] " + l.tabs[i].Name

		style := l.styles.subtitle
		if i == l.at {
			style = l.styles.title
		}

		parts = append(parts, style.Render(label))
	}

	return strings.Join(parts, "  ")
}
