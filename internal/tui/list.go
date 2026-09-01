package tui

import (
	"fmt"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Row is one line of a list screen. The four columns are laid out by the
// screen rather than by the caller, so two lists built from different data
// still align the same way.
type Row struct {
	// Key identifies the row to the caller. It is opaque here: the screen
	// hands it back with whatever action was pressed and never reads it.
	Key string

	Left  string
	Mid   string
	Age   string
	Tail  string
	Under string

	// Unread marks a row that has moved since it was last read, which is the
	// one distinction a queue exists to draw.
	Unread bool
	// Detail is the whole conversation, shown when the row is expanded. A list
	// whose rows carry none is not expandable.
	Detail []string
}

// Section is a group of rows under a heading. An empty section keeps its
// heading, because "nothing is waiting on you" is the answer most worth seeing.
type Section struct {
	Name string
	Rows []Row
}

// Action is what was pressed on a row.
type Action int

// The actions a list screen offers. Each is passed to the caller, which decides
// what it means: the screen itself changes nothing outside the frame.
const (
	// ActChoose is enter. On a list of staged reviews it opens one, which means
	// leaving this screen, so the caller may ask for the program to stop.
	ActChoose Action = iota
	// ActMark is space: this row has been read as it stands.
	ActMark
	// ActBrowse is o: open the row on GitHub.
	ActBrowse
	// ActReply is r: answer the conversation under the cursor.
	ActReply
	// ActResolve is R: mark the conversation dealt with.
	ActResolve
	// ActRefresh is ctrl+r: read the queue again.
	ActRefresh
)

// Act performs an action on a row. What it returns is the one line the footer
// shows, so it stays short enough to survive a narrow frame. Returning done
// leaves the screen, which is how choosing a review hands control back.
type Act func(a Action, r *Row) (status string, done bool, err error)

// listKeys are the keys a list screen adds to the shared movement keys. The
// letters are the first letter of the verb, and none is chorded except the
// refresh, because a queue is read far more often than it is re-read.
type listKeys struct {
	Mark    key.Binding
	Browse  key.Binding
	Reply   key.Binding
	Resolve key.Binding
	Refresh key.Binding
	Section key.Binding
}

func defaultListKeys() listKeys {
	return listKeys{
		Mark:    key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "read")),
		Browse:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "on GitHub")),
		Reply:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
		Resolve: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "resolve")),
		Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
		Section: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next group")),
	}
}

// List is a queue screen: sections of rows, one cursor, and a set of actions
// the caller supplies. It backs both the conversation queue and the list of
// staged reviews, which are the same shape and would otherwise be two screens
// that drift apart.
type List struct {
	title string
	// subtitle and sections are read again on every rebuild, because an action
	// can move a row between sections or change the count in the header, and a
	// snapshot taken at construction would show neither.
	subtitle func() string
	sections func() []Section
	act      Act

	// current is what the provider last answered. The rendered lines point into
	// it, so it is held rather than discarded.
	shown []Section

	// lines is the rendered body, rebuilt whenever anything changes. Rows and
	// headings share it so the cursor moves through what is on screen rather
	// than through a model of it.
	lines  []listLine
	cursor int
	offset int
	width  int
	height int

	expanded map[string]bool

	keys   keyMap
	list   listKeys
	styles styles

	hints     [][2]string
	helpLines []string

	status  string
	failed  bool
	help    bool
	chosen  string
	failure error
}

// listLine is one rendered line: either a heading, or a row and which part of
// it this line carries.
type listLine struct {
	heading string
	row     *Row
	// under marks the second line of a row, which quotes what was last said.
	under bool
	// detail marks a line of an expanded row.
	detail string
}

// NewList builds a list screen. The caller keeps ownership of the sections and
// hands them back on every refresh.
func NewList(title string, sections func() []Section, act Act) *List {
	l := &List{
		title: title, sections: sections, act: act,
		keys: defaultKeyMap(), list: defaultListKeys(), styles: newStyles(),
		width: minWidth, height: startHeight, expanded: map[string]bool{},
	}
	l.rebuild()

	return l
}

// WithHelp replaces the help overlay's body, so each list documents the keys it
// actually offers.
func (l *List) WithHelp(lines []string) *List {
	l.helpLines = lines

	return l
}

// WithHints replaces the keys the footer advertises, so a list that offers no
// reply does not promise one.
func (l *List) WithHints(hints [][2]string) *List {
	l.hints = hints

	return l
}

// WithSubtitle puts a count or a filter in the header's right-hand corner. It
// is a function because the count changes as rows are answered.
func (l *List) WithSubtitle(s func() string) *List {
	l.subtitle = s

	return l
}

// Chosen is the row enter was pressed on, empty when the screen was left any
// other way.
func (l *List) Chosen() string { return l.chosen }

// Failure is the action that did not complete, so a failed resolve reaches
// stdout and the exit code rather than only a footer the screen took with it.
func (l *List) Failure() error { return l.failure }

// RunList opens a list screen and blocks until the person leaves it. It returns
// the screen so the caller can read what was chosen, and the error a failed
// action left behind, which a footer the alternate screen took with it is not.
func RunList(l *List) (*List, error) {
	final, err := tea.NewProgram(l).Run()
	if err != nil {
		return l, fmt.Errorf("running the %s screen: %w", l.title, err)
	}

	if got, ok := final.(*List); ok {
		return got, got.failure
	}

	return l, nil
}

// Init lays out the first frame at the assumed size, which the terminal
// corrects with a resize before anything is drawn.
func (l *List) Init() tea.Cmd {
	l.rebuild()

	return nil
}

// Update handles a keypress or a resize. Everything else is the caller's, which
// is why the screen holds no context and starts no work of its own.
func (l *List) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.rebuild()

		return l, nil
	case tea.KeyPressMsg:
		return l.handleKey(msg)
	}

	return l, nil
}

func (l *List) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if l.help {
		l.help = false

		return l, nil
	}

	switch {
	case key.Matches(msg, l.keys.Quit):
		return l, tea.Quit
	case key.Matches(msg, l.keys.Help):
		l.help = true

		return l, nil
	case key.Matches(msg, l.keys.Down):
		l.move(1)
	case key.Matches(msg, l.keys.Up):
		l.move(-1)
	case key.Matches(msg, l.keys.HalfDown):
		l.move(l.half())
	case key.Matches(msg, l.keys.HalfUp):
		l.move(-l.half())
	case key.Matches(msg, l.keys.Top):
		l.to(0)
	case key.Matches(msg, l.keys.Bottom):
		l.to(len(l.lines) - 1)
	case key.Matches(msg, l.list.Section):
		l.nextSection()
	default:
		return l.perform(msg)
	}

	return l, nil
}

// perform runs an action on the row under the cursor. A key pressed on a
// heading does nothing rather than acting on whichever row happens to follow.
func (l *List) perform(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	row := l.current()
	if row == nil {
		return l, nil
	}

	// Expanding is the screen's own, since reading a conversation is not an
	// action anyone takes outside the frame.
	if key.Matches(msg, l.keys.Accept) && len(row.Detail) > 0 {
		l.expanded[row.Key] = !l.expanded[row.Key]
		l.rebuild()

		if l.act == nil {
			return l, nil
		}

		return l.run(ActMark, row)
	}

	for _, m := range []struct {
		binding key.Binding
		action  Action
	}{
		{l.keys.Accept, ActChoose},
		{l.list.Mark, ActMark},
		{l.list.Browse, ActBrowse},
		{l.list.Reply, ActReply},
		{l.list.Resolve, ActResolve},
		{l.list.Refresh, ActRefresh},
	} {
		if key.Matches(msg, m.binding) {
			return l.run(m.action, row)
		}
	}

	return l, nil
}

func (l *List) run(a Action, row *Row) (tea.Model, tea.Cmd) {
	if l.act == nil {
		l.status, l.failed = "nothing here can do that", true

		return l, nil
	}

	status, done, err := l.act(a, row)
	if err != nil {
		l.status, l.failed = err.Error(), true

		return l, nil
	}

	l.status, l.failed = status, false

	if done {
		l.chosen = row.Key

		return l, tea.Quit
	}

	l.rebuild()

	return l, nil
}

// current is the row the cursor is on, or nil on a heading.
func (l *List) current() *Row {
	if l.cursor < 0 || l.cursor >= len(l.lines) {
		return nil
	}

	return l.lines[l.cursor].row
}

// move steps the cursor by n lines, skipping headings and the lines that
// continue a row, so one press moves one row.
func (l *List) move(n int) {
	if n == 0 || len(l.lines) == 0 {
		return
	}

	step := 1
	if n < 0 {
		step = -1
	}

	for range abs(n) {
		next := l.cursor

		for {
			next += step
			if next < 0 || next >= len(l.lines) {
				return
			}

			if l.selectable(next) {
				break
			}
		}

		l.to(next)
	}
}

func (l *List) selectable(i int) bool {
	return l.lines[i].row != nil && !l.lines[i].under && l.lines[i].detail == ""
}

// to puts the cursor on a line, moving forward to the next selectable one when
// the target is a heading, and keeps the frame around it.
func (l *List) to(i int) {
	if len(l.lines) == 0 {
		return
	}

	i = min(max(i, 0), len(l.lines)-1)

	if !l.selectable(i) {
		for j := i; j < len(l.lines); j++ {
			if l.selectable(j) {
				i = j

				break
			}
		}
	}

	if !l.selectable(i) {
		for j := i; j >= 0; j-- {
			if l.selectable(j) {
				i = j

				break
			}
		}
	}

	l.cursor = i
	l.scroll()
}

// nextSection jumps to the first row of the next heading, so a queue is walked
// a bucket at a time when the bucket is what you are triaging.
func (l *List) nextSection() {
	for i := l.cursor + 1; i < len(l.lines); i++ {
		if l.lines[i].heading == "" {
			continue
		}

		l.to(i)
		// A heading is worth reading with the rows under it, so the jump puts
		// it at the top of the frame rather than wherever the scroll left it.
		l.offset = min(max(0, i-jumpMargin), max(0, len(l.lines)-l.visible()))

		return
	}

	l.to(0)
}

// scroll keeps scrollOff lines of context between the cursor and the edge of
// the frame, so what is next is visible before the cursor reaches it.
func (l *List) scroll() {
	body := l.visible()
	if body <= 0 {
		return
	}

	if l.cursor < l.offset+scrollOff {
		l.offset = max(0, l.cursor-scrollOff)
	}

	if l.cursor >= l.offset+body-scrollOff {
		l.offset = min(max(0, len(l.lines)-body), l.cursor-body+1+scrollOff)
	}

	l.offset = min(l.offset, max(0, len(l.lines)-body))
}

// half is a half-page, which is what ctrl+u and ctrl+d move.
func (l *List) half() int {
	const halves = 2

	return l.visible() / halves
}

// visible is how many lines the frame has for rows, after the header and footer.
func (l *List) visible() int {
	const chrome = 3

	return max(0, l.height-chrome)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// widest measures the two columns that vary, so rows line up with each other.
func (l *List) widest() (int, int) {
	const (
		leftCap = 34
		midCap  = 48
	)

	var left, mid int

	for i := range l.shown {
		for j := range l.shown[i].Rows {
			left = max(left, min(leftCap, textWidth(l.shown[i].Rows[j].Left)))
			mid = max(mid, min(midCap, textWidth(l.shown[i].Rows[j].Mid)))
		}
	}

	return left, mid
}

// rebuild lays the sections out as lines. It runs whenever anything that
// changes the frame changes, so the cursor and the scroll offset always index
// what is actually on screen.
func (l *List) rebuild() {
	was := l.current()

	l.lines = l.lines[:0]
	l.shown = l.sections()

	for i := range l.shown {
		s := &l.shown[i]

		l.lines = append(l.lines, listLine{heading: heading(s)})

		for i := range s.Rows {
			row := &s.Rows[i]

			l.lines = append(l.lines, listLine{row: row})

			if row.Under != "" {
				l.lines = append(l.lines, listLine{row: row, under: true})
			}

			if !l.expanded[row.Key] {
				continue
			}

			for _, d := range row.Detail {
				l.lines = append(l.lines, listLine{row: row, detail: d})
			}
		}
	}

	if was == nil {
		l.to(0)

		return
	}

	for i, line := range l.lines {
		if line.row != nil && line.row.Key == was.Key && !line.under && line.detail == "" {
			l.cursor = i
			l.scroll()

			return
		}
	}

	l.to(l.cursor)
}

func heading(s *Section) string {
	return s.Name + " (" + strconv.Itoa(len(s.Rows)) + ")"
}
