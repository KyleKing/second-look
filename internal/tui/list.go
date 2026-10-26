package tui

import (
	"strconv"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/kyleking/aragonite/tui/frame"
	"github.com/kyleking/aragonite/tui/overlay"
	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/labels"
)

// Row is one line of a list screen. The columns are laid out by the screen
// rather than by the caller, so two lists built from different data still align
// the same way.
type Row struct {
	// Key identifies the row to the caller. It is opaque here: the screen
	// hands it back with whatever action was pressed and never reads it.
	Key string

	// Kind is what sort of row this is, matched by a kind: word in the filter.
	// A queue of conversations holds three kinds, and answering a review thread
	// is different work from answering a comment on the pull request itself.
	Kind string
	// Repo is the repository the row belongs to, which is what focus narrows
	// on. A row standing for something other than a pull request carries none.
	Repo string
	// Dim draws the whole row receded, for work that is waiting on somebody
	// else.
	Dim bool
	// Unread marks a row that has moved since it was last read, which is the
	// one distinction a queue exists to draw.
	Unread bool
	// Stale marks a row drawn from a snapshot that the forge has not confirmed
	// this run. It recedes the cells that carry Fact and nothing else.
	Stale bool
	// Flash is the row having moved lane or rank, or being new since the last
	// order drawn: 0 is settled and 1 to 4 is the step it has decayed to. The
	// screen sets it and the row draws it.
	Flash uint8
	// Cells is the row on the tab's grid, keyed by the Key of the column each
	// fills. A column the row carries no cell for draws blank.
	Cells map[string]Cell
	// About is what the inspector says of the row. A row carrying none leaves
	// it blank.
	About *About
	// Detail is the whole conversation, shown when the row is expanded. A list
	// whose rows carry none is not expandable.
	Detail []string
}

// Section is a group of rows under a heading. An empty section keeps its
// heading, because "nothing is waiting on you" is the answer most worth seeing.
type Section struct {
	Name string
	// Note stands in for the row count in the heading, for a section that
	// cannot count its rows yet. A queue still searching says so rather than
	// reading as a queue with nothing in it.
	Note string
	Rows []Row
	// Fold draws the heading and its note and none of the rows.
	Fold bool
	// Recedes draws the heading in quieter words, for a section that is waiting
	// on somebody else or already done.
	Recedes bool
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
	// ActCheckout is C: move a working copy onto the row's pull request.
	ActCheckout
	// ActComment is m: say something on the pull request itself, outside any
	// review.
	ActComment
	// ActApprove is A: approve the row's pull request. The caller decides what
	// ceremony that takes, since this screen sends nothing itself.
	ActApprove
	// ActDiscard is d: throw the row's local state away. The screen asks
	// nothing first, so a caller that cannot undo it confirms its own way.
	ActDiscard
	// ActSort is s: advance to the next row order. The screen decides what
	// orders exist and which is next; this list only asks for the change.
	ActSort
	// ActMerge is M: merge the row's pull request. The caller decides what
	// ceremony that takes.
	ActMerge
	// ActFold is z: show or hide the section the caller keeps folded.
	ActFold
)

// Act performs an action on a row. What it returns is the one line the footer
// shows, so it stays short enough to survive a narrow frame. Returning done
// leaves the screen, which is how choosing a review hands control back.
type Act func(a Action, r *Row) (status string, done bool, err error)

// listKeys are the keys a list screen adds to the shared movement keys. The
// letters are the first letter of the verb, and none is chorded except the
// refresh, because a queue is read far more often than it is re-read.
type listKeys struct {
	Mark     key.Binding
	Browse   key.Binding
	Reply    key.Binding
	Resolve  key.Binding
	Refresh  key.Binding
	Section  key.Binding
	Checkout key.Binding
	Comment  key.Binding
	Approve  key.Binding
	Discard  key.Binding
	Sort     key.Binding
	Fold     key.Binding
	Focus    key.Binding
	Unfocus  key.Binding
	Unread   key.Binding
	UnreadUp key.Binding
}

func defaultListKeys() listKeys {
	return listKeys{
		Mark:    key.NewBinding(key.WithKeys(spaceKey), key.WithHelp(spaceKey, "read")),
		Browse:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "on GitHub")),
		Reply:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
		Resolve: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "resolve")),
		Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
		Section: key.NewBinding(key.WithKeys(tabKey), key.WithHelp(tabKey, "next group")),
		// C matches the review screen's own checkout key, and m and A are the
		// first letter of the verb rather than gh-dash's, since c is taken.
		Checkout: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "check out")),
		Comment:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "comment")),
		Approve:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "approve")),
		Discard:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "discard")),
		Sort:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Fold:     key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "merged")),
		Focus:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "focus")),
		Unfocus:  key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "every repository")),
		Unread:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "next unread")),
		UnreadUp: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "last unread")),
	}
}

// Loader feeds a list its rows as they arrive rather than before it opens. A
// queue that runs four independent searches has no reason to show nothing until
// the slowest answers, and an empty terminal for six seconds reads as a hang.
//
// Start is run when the screen opens and again on a refresh; whatever it
// returns is a bubbletea command like any other. Absorb takes the messages
// those commands produce, writes them wherever the caller keeps its rows, and
// reports whether the message was one of its own.
type Loader interface {
	Start() tea.Cmd
	Absorb(msg tea.Msg) (tea.Cmd, bool)
}

// List is a queue screen: sections of rows, one cursor, and a set of actions
// the caller supplies. It backs both the conversation queue and the list of
// staged reviews, which are the same shape and would otherwise be two screens
// that drift apart.
type List struct {
	title string
	// state and sections are read again on every rebuild, because an action
	// can move a row between sections or change the state in the header, and a
	// snapshot taken at construction would show neither.
	state    func() State
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
	filter   filter
	// focused is the repository the whole screen is narrowed to, which every
	// tab shares and a refresh keeps, and focusNote is what the caller said
	// about it.
	focused   string
	focusNote string
	onFocus   func(repo string) tea.Cmd
	// onFocusChange runs when focus leaves a repository, so what the sitting
	// claimed for it can be handed back.
	onFocusChange func(from, to string) tea.Cmd
	loader        Loader
	labeling      Labeling
	labeler       *labelPicker
	labelRow      string
	labeled       func(r *Row) (labels.Target, []string, bool)
	relabel       func(row, name string, on bool)
	// onRest is called with the row the cursor has stopped on, and moves counts
	// the moves so far, so an answer about a row already left is dropped.
	onRest func(row string) tea.Cmd
	moves  int
	// touched marks a cursor the reader has moved. Until then a queue filling
	// in as its searches answer keeps the cursor at the top, since a row that
	// arrives above the cursor should not leave it in the middle of the list.
	touched bool

	// held is a refreshed order waiting on the keyboard. baseline is the rows as
	// last drawn and shifts the ones the last refresh moved.
	now      func() time.Time
	lastKey  time.Time
	held     bool
	baseline []Place
	shifts   map[string]Move
	// flash is each moved row's step and flashGen numbers the tick decaying
	// them, so a tick from an earlier refresh is dropped. armed says the tab has
	// drawn a snapshot, which is what an order is later compared against.
	flash    map[string]uint8
	flashGen int
	armed    bool

	keys   keyMap
	list   listKeys
	styles styles

	hints     [][2]string
	inert     func(r *Row) []string
	helpLines [][2]string

	// tabs is the other queues this screen holds, empty for a screen that is
	// only itself. at is which one is being read and views is where each of the
	// others was left.
	tabs  []Tab
	at    int
	views []view
	// columns is the current tab's grid.
	columns func() []table.Column

	// mono is a terminal with no color, where the row under the cursor is drawn
	// reversed because no band can be.
	mono bool

	status string
	failed bool
	help   bool
	helpAt int
	// helpBox is where the legend was last drawn, which a click is read against.
	helpBox overlay.Rect
	chosen  string
	failure error
}

// listLine is one rendered line: either a heading, or a row and which part of
// it this line carries.
type listLine struct {
	heading string
	// section is the group a heading line draws, and blank marks the empty line
	// that sets a heading apart from the rows above it.
	section *Section
	blank   bool
	row     *Row
	// detail marks a line of an expanded row.
	detail string
}

// WithLabeling lets l pick labels for the rows whose tab says what they carry.
func (l *List) WithLabeling(lb Labeling) *List {
	l.labeling = lb

	return l
}

// restMsg is the cursor having held still long enough that what is under it is
// worth paying for.
type restMsg struct{ at int }

// settleFor is how long the cursor holds still before what is under it is paid
// for. Long enough that running down a queue asks for nothing, short enough
// that stopping on a row and looking at it reads as immediate.
const settleFor = 400 * time.Millisecond

// rest starts the wait again. The count is what tells an answer about the row
// under the cursor from one about a row the reader has already left.
func (l *List) rest() tea.Cmd {
	if l.onRest == nil {
		return nil
	}

	l.moves++
	at := l.moves

	return tea.Tick(settleFor, func(time.Time) tea.Msg { return restMsg{at: at} })
}

// settled answers the wait, and does nothing where the cursor has moved since.
func (l *List) settled(at int) tea.Cmd {
	row := l.current()
	if at != l.moves || row == nil {
		return nil
	}

	return l.onRest(row.Key)
}

// Chosen is the row enter was pressed on, empty when the screen was left any
// other way.
func (l *List) Chosen() string { return l.chosen }

// Failure is the action that did not complete, so a failed resolve reaches
// stdout and the exit code rather than only a footer the screen took with it.
func (l *List) Failure() error { return l.failure }

// say puts a line in the footer, which is where a list reports what an action
// did or why it could not.
func (l *List) say(status string, failed bool) { l.status, l.failed = status, failed }

// Init lays out the first frame at the assumed size, which the terminal
// corrects with a resize before anything is drawn.
func (l *List) Init() tea.Cmd {
	cmd := l.StartAll()
	l.rebuild()

	return tea.Batch(cmd, l.rest())
}

// setProfile is where the terminal's color support reaches the list: with none,
// rows are marked by attribute rather than by color.
func (l *List) setProfile(p colorprofile.Profile) {
	l.mono = p <= colorprofile.ASCII
	if l.mono {
		l.styles.monochrome()
	}
}

// Update handles a keypress or a resize. Everything else is the caller's, which
// is why the screen holds no context and starts no work of its own.
func (l *List) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.rebuild()

		return l, nil
	case tea.MouseMsg:
		cmd := l.mouse(msg)

		return l, cmd
	case tea.ColorProfileMsg:
		l.setProfile(msg.Profile)

		return l, nil
	case tea.KeyPressMsg:
		l.lastKey = l.clock()

		return l.handleKey(msg)
	case holdMsg:
		cmd := l.idle()

		return l, cmd
	case flashMsg:
		cmd := l.decay(msg.gen)

		return l, cmd
	case restMsg:
		cmd := l.settled(msg.at)

		return l, cmd
	case labelsReadMsg:
		l.labeler.hear(msg)

		return l, nil
	case labelSetMsg:
		l.applyLabelSet(msg)

		return l, nil
	case FocusNote:
		if msg.Repo == l.focused {
			l.focusNote = msg.Note
		}

		return l, nil
	case StatusMsg:
		l.say(msg.Text, msg.Failed)

		return l, nil
	}

	// Every tab's loader is fed, not only the one being read: a queue switched
	// away from mid-search still has answers coming, and dropping them would
	// leave it half full whenever it is switched back to.
	// The wait starts again on every answer as well as on every move: the row
	// the cursor is on when a search lands is one nobody moved to, and it is
	// still the row being looked at.
	for _, ld := range l.loaders() {
		if cmd, mine := ld.Absorb(msg); mine {
			return l, tea.Batch(cmd, l.apply(), l.rest())
		}
	}

	return l, nil
}

func (l *List) loaders() []Loader {
	out := make([]Loader, 0, len(l.tabs))

	for i := range l.tabs {
		if l.tabs[i].Loader != nil {
			out = append(out, l.tabs[i].Loader)
		}
	}

	return out
}

func (l *List) mouse(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return l.wheel(msg)
	case tea.MouseClickMsg:
		return l.click(msg.Mouse())
	}

	return nil
}

// wheel scrolls the legend while it is up, and moves the cursor otherwise. A
// wheel tick is not a key, so the legend's any-key-closes does not answer it.
func (l *List) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	step := 0

	switch msg.Button {
	case tea.MouseWheelUp:
		step = -wheelStep
	case tea.MouseWheelDown:
		step = wheelStep
	}

	if l.help {
		l.helpAt = max(0, l.helpAt+step)

		return nil
	}

	if step == 0 || l.labeler != nil || l.filter.typing {
		return nil
	}

	l.move(step)

	return l.rest()
}

// click closes the legend from outside it, switches to a tab clicked on, and
// puts the cursor on a row clicked on. A prompt in the footer keeps the screen.
func (l *List) click(m tea.Mouse) tea.Cmd {
	if l.help {
		if !l.helpBox.Contains(m.X, m.Y) {
			l.help = false
		}

		return nil
	}

	if m.Button != tea.MouseLeft || l.labeler != nil || l.filter.typing {
		return nil
	}

	f := l.frame()

	if at, ok := frame.TabAt(l.styles.skin, f, l.width, m.X, m.Y); ok {
		return l.switchTo(at)
	}

	row, ok := frame.BodyRow(f, l.height, m.Y)
	if !ok || l.offset+row >= len(l.lines) || !l.selectable(l.offset+row) {
		return nil
	}

	l.cursor = l.offset + row
	l.scroll()

	return l.rest()
}

// readHelp answers a key while the legend is up: the scroll keys move inside
// its box, and anything else puts the legend away.
func (l *List) readHelp(msg tea.KeyPressMsg) {
	const halfPage = 2

	switch msg.String() {
	case "up":
		l.helpAt--
	case "down":
		l.helpAt++
	case "pgup":
		l.helpAt -= l.height / halfPage
	case "pgdown":
		l.helpAt += l.height / halfPage
	case "home":
		l.helpAt = 0
	case "end":
		l.helpAt = pastEnd
	default:
		l.help, l.helpAt = false, 0
	}

	l.helpAt = max(0, l.helpAt)
}

func (l *List) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if l.help {
		l.readHelp(msg)

		return l, nil
	}

	if l.labeler != nil {
		return l.pickLabel(msg)
	}

	if l.filter.typing {
		return l.typeFilter(msg)
	}

	switch {
	case key.Matches(msg, l.keys.Quit):
		cmd := l.leave(msg)

		return l, cmd
	case key.Matches(msg, l.keys.Search):
		cmd := l.beginFilter()

		return l, cmd
	case key.Matches(msg, l.keys.Help):
		l.help = true

		return l, nil
	}

	if cmd, mine := l.focusKey(msg); mine {
		return l, cmd
	}

	if cmd, ok := l.tabKey(msg); ok {
		return l, cmd
	}

	if l.moved(msg) {
		cmd := l.rest()

		return l, cmd
	}

	// A screen that fills in as its searches answer re-runs them itself, since
	// the row under the cursor has nothing to do with reading the queue again.
	if l.loader != nil && key.Matches(msg, l.list.Refresh) {
		l.armed = true
		cmd := l.loader.Start()
		l.status, l.failed = "", false

		return l, cmd
	}

	return l.perform(msg)
}

// perform runs an action on the row under the cursor. A key pressed on a
// heading does nothing rather than acting on whichever row happens to follow.
func (l *List) perform(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	row := l.current()
	if row == nil {
		return l, nil
	}

	if key.Matches(msg, l.keys.Label) {
		cmd := l.openLabels(row)

		return l, cmd
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
		{l.list.Checkout, ActCheckout},
		{l.list.Comment, ActComment},
		{l.list.Approve, ActApprove},
		{l.list.Discard, ActDiscard},
		{l.list.Sort, ActSort},
		{l.list.Fold, ActFold},
		{l.keys.Merge, ActMerge},
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

		// What leaving means is the shell's: a chosen review opens in place and
		// everything else gives the terminal back.
		return l, func() tea.Msg { return chosenMsg{} }
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
	return l.lines[i].row != nil && l.lines[i].detail == ""
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

// nextUnread jumps to the next row carrying the unread mark, wrapping at the
// ends, which is the motion a queue is triaged with: u, read or act, u again.
func (l *List) nextUnread(step int) {
	n := len(l.lines)
	if n == 0 {
		return
	}

	for d := step; d*step <= n; d += step {
		i := ((l.cursor+d)%n + n) % n
		if l.selectable(i) && l.lines[i].row.Unread {
			l.to(i)

			return
		}
	}
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

// peek scrolls the frame and leaves the cursor, so a glance further down the
// queue costs nothing to come back from: the next motion pulls the frame to the
// cursor, the way it does in the review screen. A frame that cannot move says
// so, since the same key on a list that fits reads as dead.
func (l *List) peek(step int) {
	at := l.offset
	l.offset = clamp(at+step, max(0, len(l.lines)-l.visible()))

	if l.offset == at {
		l.say("nothing further that way", false)
	}
}

// half is a half-page, which is what ctrl+u and ctrl+d move.
func (l *List) half() int {
	const halves = 2

	return l.visible() / halves
}

// visible is how many lines the frame has for rows, after the header and the
// panel pinned to the bottom.
func (l *List) visible() int {
	return frame.Rows(l.frame(), l.height)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// rebuild lays the sections out as lines. It runs whenever anything that
// changes the frame changes, so the cursor and the scroll offset always index
// what is actually on screen.
func (l *List) rebuild() {
	was := l.current()

	l.lines = l.lines[:0]
	l.shown = l.filter.narrow(keepFocused(l.focused, l.sections()))
	l.baseline = placesOf(l.shown)
	l.armed = l.armed || anyStale(l.shown)
	l.stamp()

	for i := range l.shown {
		l.appendSection(i, &l.shown[i])
	}

	if was == nil || !l.touched {
		l.to(0)

		return
	}

	for i, line := range l.lines {
		if line.row != nil && line.row.Key == was.Key && line.detail == "" {
			l.cursor = i
			l.scroll()

			return
		}
	}

	l.to(l.cursor)
}

// roomyHeight is how many rows the terminal needs before a blank line sets
// each group apart: below it the room goes to the rows.
const roomyHeight = 30

// appendSection lays out the i'th section: its heading, then its rows and
// whatever each has expanded.
func (l *List) appendSection(i int, s *Section) {
	if i > 0 && l.height >= roomyHeight {
		l.lines = append(l.lines, listLine{blank: true})
	}

	l.lines = append(l.lines, listLine{heading: heading(s), section: s})

	if s.Fold {
		return
	}

	for j := range s.Rows {
		row := &s.Rows[j]

		l.lines = append(l.lines, listLine{row: row})

		if !l.expanded[row.Key] {
			continue
		}

		for _, d := range row.Detail {
			l.lines = append(l.lines, listLine{row: row, detail: d})
		}
	}
}

// moved handles every key that only changes where the frame is looking, so the
// keys that do something to a row stay a short list.
func (l *List) moved(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, l.keys.PeekDown):
		l.peek(1)
	case key.Matches(msg, l.keys.PeekUp):
		l.peek(-1)
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
	case key.Matches(msg, l.list.Unread):
		l.nextUnread(1)
	case key.Matches(msg, l.list.UnreadUp):
		l.nextUnread(-1)
	default:
		return false
	}

	l.touched = true

	return true
}

// leave answers q and esc. Escape puts a narrowed queue back rather than
// leaving, since a filter is a state to get out of; q means what it says either
// way.
func (l *List) leave(msg tea.KeyPressMsg) tea.Cmd {
	if l.filter.on() && key.Matches(msg, l.keys.Back) {
		l.clearFilter()

		return nil
	}

	return tea.Quit
}

func heading(s *Section) string {
	if s.Fold {
		return s.Name + " (" + strconv.Itoa(len(s.Rows)) + ", z shows them)"
	}

	if s.Note != "" {
		return s.Name + " (" + s.Note + ")"
	}

	return s.Name + " (" + strconv.Itoa(len(s.Rows)) + ")"
}
