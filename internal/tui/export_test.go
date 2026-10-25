package tui

import (
	"image/color"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/theme"
)

// Frame returns one rendered screen, so a test can check the layout without a
// terminal.
func (m *Model) Frame() string { return m.render() }

// HeadChecked delivers what the head check answered, which Init asks behind the
// first frame and a test has no program loop to run for it.
func (m *Model) HeadChecked(sha string) {
	m.applyHead(headMsg{sha: sha, want: m.review.HeadSHA})
}

// HeadFailed delivers a head check that could not reach the forge.
func (m *Model) HeadFailed(err error) {
	m.applyHead(headMsg{want: m.review.HeadSHA, err: err})
}

// Failure is the submit that did not post, which Run leaves through.
func (m *Model) Failure() error { return m.failure }

// CommentUnderCursor is which comment the cursor is inside, or -1.
func (m *Model) CommentUnderCursor() int { return m.current() }

// CommentStatus is what a comment is stamped, read from the review the screen
// holds rather than from the file, so a test can tell a refused keystroke from
// one that changed nothing on disk.
func (m *Model) CommentStatus(i int) string { return m.review.Comments[i].Status }

// CursorRow is which row the cursor is on, so a test can check where a motion
// landed rather than inferring it from the frame.
func (m *Model) CursorRow() int { return m.cursor }

// CursorFile is the file the row under the cursor belongs to.
func (m *Model) CursorFile() string { return m.cursorPath() }

// CursorText is what the row under the cursor says.
func (m *Model) CursorText() string { return rowText(m.screen.rows[m.cursor]) }

// SetSender supplies the single-comment poster after construction, which is
// what a test needs when the sender has to see the model's own review.
func (m *Model) SetSender(s Sender) { m.send = s }

// ListFrame returns one rendered list screen, so a test can check the layout
// without a terminal.
func (l *List) ListFrame() string { return l.render() }

// Frame is the screen the shell is drawing, so a test can say which of the two
// it has on without a terminal.
func (s *Shell) Frame() string {
	if s.at == modeReview {
		return s.review.render()
	}

	return s.list.render()
}

// CursorKey is the row the cursor is on, so a test can check where a motion
// landed rather than inferring it from the frame.
func (l *List) CursorKey() string {
	if row := l.current(); row != nil {
		return row.Key
	}

	return ""
}

// WantsCheckout is C, which the caller answers once the screen has closed.
func (m *Model) WantsCheckout() bool { return m.checkout }

// DiffColors is everything a change is drawn out of: the frame the bands sit
// on, the color the code is written in, and the four backgrounds themselves at
// the depths a terminal of the given kind gets. A test measures them against
// each other, which is the only way to know a band says anything.
func DiffColors(p theme.Palette, millions bool) map[string]color.Color {
	band, mark := depths(millions)

	return map[string]color.Color{
		"page":         p.Base,
		"text":         p.Text,
		"added band":   theme.Tint(p.Base, p.Green, band),
		"removed band": theme.Tint(p.Base, p.Red, band),
		"added mark":   theme.Tint(p.Base, p.Green, mark),
		"removed mark": theme.Tint(p.Base, p.Red, mark),
	}
}

// Reload delivers the watcher's message without waiting on its timer, so a test
// can pin what a write from an agent does to the screen.
func (m *Model) Reload(path string) {
	at, _ := stampOf(path)
	m.reloaded(reloadMsg{at: at})
}

// SetRestage supplies the restager after construction, which is what a test
// needs when the answer has to name the model's own review.
func (m *Model) SetRestage(r Restager) { m.restage = r }

// SawHead is the watcher having found a head that moved, so a test can reach
// the restage without waiting out the check.
func (m *Model) SawHead(sha string) { m.newHead = sha }

// CursorAnchor is the line of the file the cursor is standing on, so a test can
// name the range it opened rather than counting keystrokes.
func (m *Model) CursorAnchor() int {
	r := m.screen.rows[m.cursor]

	return anchorOf(r.path, r.line).line
}

// Armed is the number the last cursor move gave the settle timer, so a test can
// deliver that timer without waiting one out.
func (l *List) Armed() int { return l.moves }

// Settle delivers the settle timer armed at at, which is what the cursor
// stopping on a row runs.
func (l *List) Settle(at int) tea.Cmd { return l.settled(at) }

// SetRounds supplies the earlier-round reader after construction, which is what
// a test needs when the diff it answers with is built beside the review.
func (m *Model) SetRounds(r Rounds) { m.rounds = r }

// DimKeys is which footer keys do nothing where the cursor is, which is what
// the frame draws dim and a narrow frame drops.
func (m *Model) DimKeys() []string {
	var out []string

	for _, h := range m.hints() {
		if h.off {
			out = append(out, h.key)
		}
	}

	return out
}

// Inert reports a legend key with nothing to act on under the cursor.
func (m *Model) KeyIsInert(key string) bool { return m.inert(key) }

// ChordWaited fires the wait a chord armed when it opened, so a test sees the
// page it draws without sleeping for it.
func (m *Model) ChordWaited() { m.Update(chordWaitMsg{seq: m.chordSeq}) }

// PaneClosed delivers the pane's exit the way the program loop would, so a
// test reaches the teardown path without racing the child's exit.
func (m *Model) PaneClosed(err error) { m.paneGone(err) }

// Relayout lays out rows for the current view from the whole diff, which is
// what a fold, toggle, or order change runs again rather than only for the
// visible window.
func (m *Model) Relayout() { m.rebuild() }

// Restructure runs the structural pass over the whole diff, one subprocess per
// hunk side, and applies its answer as the command landing behind the first
// frame does.
func (m *Model) Restructure() {
	if msg, ok := readStructure(m.diff, m.made)().(structureMsg); ok {
		m.applyStructure(msg)
	}
}

// Checked delivers the pass over the review's files, which Init asks behind the
// first frame and a test has no program loop to run for it.
func (m *Model) Checked() {
	cmd := m.probe()
	if cmd == nil {
		return
	}

	if msg, ok := cmd().(notesMsg); ok {
		m.applyNotes(msg)
	}
}

// Probed delivers the agent listing's answer the way Init's command landing
// does, since a test has no program loop to run it through.
func (m *Model) Probed() {
	cmd := m.probeAgent()
	if cmd == nil {
		return
	}

	if msg, ok := cmd().(agentStateMsg); ok {
		m.agentState = msg.state
	}
}

// Versioned runs the registry asks Init batches and feeds their answers back
// the way the program loop would, so a test sees the cards without a network.
func (m *Model) Versioned() {
	for _, cmd := range m.fetchVersions() {
		if cmd != nil {
			m.Update(cmd())
		}
	}
}

// ShortestTail is the name the header gives a file among the others in the diff.
var ShortestTail = shortestTail

// SearchHead runs the head searches Init asked for behind the first frame.
func (m *Model) SearchHead() {
	for needle, h := range m.atHead {
		if !h.done {
			hits, err := m.look(m.ctx, needle)
			m.applyHeaded(headedMsg{needle: needle, hits: hits, err: err})
		}
	}
}

// StartRoundReads is the reads the H chord starts behind the frame, which a test runs
// itself because the key's own command is batched with a timer.
func (m *Model) StartRoundReads() tea.Cmd { return m.readRounds(m.earlier()) }

// GraphNode is a function in a call graph as a test spells it.
type GraphNode struct {
	Name          string
	Times         int
	Edited, Judge bool
	Where         string
}

// DrawGraph draws a call graph at a width and returns its lines and the one
// holding the selected node, which is the at-th of the callees or of the callers.
// The selection walks down to via first and then to at, one node at a time, so
// the side is scrolled where pressing j and k would have left it.
func DrawGraph(center GraphNode, callers, callees []GraphNode, onCallees bool, via, at, width int) ([]string, int) {
	nodes := func(in []GraphNode) []graphNode {
		out := make([]graphNode, 0, len(in))
		for _, n := range in {
			out = append(out, graphNode{name: n.Name, times: n.Times, edited: n.Edited, judge: n.Judge, where: n.Where})
		}

		return out
	}

	side := sideCallers
	if onCallees {
		side = sideCallees
	}

	v := graphView{
		center: nodes([]GraphNode{center})[0], callers: nodes(callers), callees: nodes(callees), side: side, at: at,
	}

	n := len(v.callers)
	if onCallees {
		n = len(v.callees)
	}

	cur := 0

	for _, target := range []int{via, at} {
		for cur != target {
			cur += min(max(target-cur, -1), 1)
			v.tops[side] = scrollTo(n, cur, v.tops[side])
		}
	}

	lines, row := layoutGraph(v, width)

	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.text)
	}

	return out, row
}

// Reading is the contrast of one face against what it is drawn on.
type Reading struct {
	Name  string
	Ratio float64
}

// Readings measures every face a queue draws text in against its background:
// a row's dim face on the frame, its lit face on the selection band, and the
// chips and lane headings on what they sit on.
func Readings() []Reading {
	st := loadStyles()
	base, band := st.palette.Base, st.band.GetBackground()

	var out []Reading

	add := func(name string, fg, bg color.Color) {
		out = append(out, Reading{Name: name, Ratio: theme.Contrast(fg, bg)})
	}

	for name, r := range st.rungs.all() {
		add(name+" dim on Base", r.dim.GetForeground(), base)
		add(name+" lit on the band", r.lit.GetForeground(), band)
	}

	for name, c := range map[string]*lipgloss.Style{
		"badge": &st.badge, "badge active": &st.badgeOn, "badge blocked": &st.badgeBlocked,
	} {
		add(name, c.GetForeground(), c.GetBackground())
	}

	for name, c := range map[string]*lipgloss.Style{
		"lane label": &st.lanes.label, "lane count": &st.lanes.count,
		"quiet label": &st.lanes.quietLabel, "quiet count": &st.lanes.quietCount,
		"hollow dot": &st.lanes.hollow,
	} {
		add(name, c.GetForeground(), base)
	}

	for tone := range st.lanes.dots {
		face := st.lanes.dots[tone]
		add("lane dot "+strconv.Itoa(int(tone)), face.GetForeground(), base)
	}

	return out
}
