package tui

import (
	"fmt"
	"path/filepath"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/threads"
)

// graphSites is how many call sites a node's expansion excerpts.
const graphSites = 3

// callGraph is a gc card's state: the answer, which node of it is selected, and
// how far each side has scrolled to keep that node in view.
type callGraph struct {
	graph diag.Graph
	side  int
	at    int
	tops  [2]int
	// open is whether the selected node's call sites are drawn under the graph.
	open bool
}

func newCallGraph(g diag.Graph) *callGraph {
	c := &callGraph{graph: g, side: sideCallers}
	if len(g.Callees) > 0 {
		c.side = sideCallees
	}

	return c
}

func (c *callGraph) nodes(side int) []diag.CallNode {
	if side == sideCallers {
		return c.graph.Callers
	}

	return c.graph.Callees
}

func (c *callGraph) reach(side int) int { return len(c.nodes(side)) }

func (c *callGraph) selected() (diag.CallNode, bool) {
	if c.at >= c.reach(c.side) {
		return diag.CallNode{}, false
	}

	return c.nodes(c.side)[c.at], true
}

func (c *callGraph) step(by int) {
	c.at, c.open = clamp(c.at+by, c.reach(c.side)-1), false
	c.tops[c.side] = scrollTo(c.reach(c.side), c.at, c.tops[c.side])
}

func (c *callGraph) across(side int) {
	if c.reach(side) == 0 {
		return
	}

	c.side, c.at, c.open = side, clamp(c.at, c.reach(side)-1), false
	c.tops[side] = scrollTo(c.reach(side), c.at, c.tops[side])
}

func graphKeys() [][2]string {
	return [][2]string{
		{walkKeys, "node"},
		{"h/l", "side"},
		{"za", "call sites"},
		{enterKey, openVerb},
		{"gc", "center"},
		{"ctrl+o", "back"},
	}
}

// callsMsg is a language server's call graph. Anchor is the line the card hangs
// from, and from is the card a recentering was asked of, which is nil for the
// first ask.
type callsMsg struct {
	anchor diag.Site
	from   *peekCard
	graph  diag.Graph
	err    error
}

// calls answers gc: the call graph of the function the cursor's line is in, or,
// with a graph open, the graph centered on the selected node.
func (m *Model) calls() tea.Cmd {
	if m.prober == nil {
		m.noProber()

		return nil
	}

	if c := m.graphHere(); c != nil {
		return m.recenter(c)
	}

	r := m.screen.rows[m.cursor]
	if r.kind != rowCode || r.path == "" || r.line.New == 0 {
		m.say("g's calls asks about a line of the change", false)

		return nil
	}

	site := diag.Site{Path: r.path, Line: r.line.New}

	return m.askCalls(site, site, nil)
}

func (m *Model) recenter(c *callGraph) tea.Cmd {
	n, ok := c.selected()
	if !ok {
		m.say("no node to center on", false)

		return nil
	}

	if filepath.IsAbs(n.Path) {
		m.say(n.Name+" is outside the checkout, so gc cannot center on it", false)

		return nil
	}

	anchor := diag.Site{Path: m.peeked.path, Line: m.peeked.line}

	return m.askCalls(anchor, diag.Site{Path: n.Path, Line: n.Line}, m.peeked)
}

func (m *Model) askCalls(anchor, at diag.Site, from *peekCard) tea.Cmd {
	server, ok := m.prober.Server(at.Path)
	if !ok {
		m.say("no language server for "+at.Path, false)

		return nil
	}

	m.say("asking "+server+"…", false)

	ask, ctx := m.prober, m.ctx

	return func() tea.Msg {
		g, err := ask.Calls(ctx, at.Path, at.Line)

		return callsMsg{anchor: anchor, from: from, graph: g, err: err}
	}
}

func (m *Model) applyCalls(msg callsMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		m.say(msg.err.Error(), true)
	case msg.from == nil && !m.onAnchor(msg.anchor.Path, msg.anchor.Line), msg.from != nil && m.peeked != msg.from:
		m.say("the cursor moved on before the call graph was answered", false)
	default:
		g := msg.graph
		m.jumps.push(m.snapshot())

		m.peeked = &peekCard{
			name: g.Center.Name, path: msg.anchor.Path, line: msg.anchor.Line, placing: true,
			sites: []diag.Site{{Path: g.Center.Path, Line: g.Center.Line}}, graph: newCallGraph(g),
		}
		m.say("", false)
		m.rebuild()
		m.enterPeek()
	}

	return nil
}

// graphHere is the graph under the cursor, and nil where the cursor is not on a
// gc card.
func (m *Model) graphHere() *callGraph {
	p := m.peeked
	if p == nil || p.graph == nil || m.cursor < 0 || m.cursor >= len(m.screen.rows) ||
		m.screen.rows[m.cursor].kind != rowPeek {
		return nil
	}

	return p.graph
}

// graphKey walks a gc card: j and k between the nodes of a side, h and l between
// the sides, and enter into the selected node.
func (m *Model) graphKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	c := m.graphHere()
	if c == nil {
		return nil, false
	}

	switch {
	case key.Matches(msg, m.keys.Down):
		c.step(1)
	case key.Matches(msg, m.keys.Up):
		c.step(-1)
	case key.Matches(msg, m.keys.Callers):
		c.across(sideCallers)
	case key.Matches(msg, m.keys.Callees):
		c.across(sideCallees)
	case key.Matches(msg, m.keys.Accept):
		return m.openNode(c), true
	default:
		return nil, false
	}

	m.regraph()

	return nil, true
}

// regraph redraws the card after the selection moved and puts the cursor on the
// selected node's line.
func (m *Model) regraph() {
	m.rebuild()
	m.cursor = m.graphCursor()
	m.follow()
}

// expandSites answers za in a card: the selected node's call sites, excerpted
// under the graph, or taken down again.
func (m *Model) expandSites() tea.Cmd {
	c := m.graphHere()

	n, ok := c.selected()
	if !ok {
		m.say("no node to show the call sites of", false)

		return nil
	}

	c.open = !c.open
	m.say("", false)
	m.regraph()

	if !c.open {
		return nil
	}

	return m.peekReads(n.Sites)
}

// openNode takes the reader to the selected node: its line in the diff where the
// diff draws it, and the file view otherwise.
func (m *Model) openNode(c *callGraph) tea.Cmd {
	n, ok := c.selected()
	if !ok {
		return nil
	}

	site := diag.Site{Path: n.Path, Line: n.Line}
	if m.codeRowAt(site) < 0 {
		return m.openSite(site)
	}

	m.jumps.push(m.snapshot())

	m.peeked = nil
	m.rebuild()
	m.cursor = m.codeRowAt(site)
	m.reveal()

	return nil
}

func (m *Model) codeRowAt(site diag.Site) int {
	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.kind == rowCode && r.path == site.Path && r.line.New == site.Line {
			return i
		}
	}

	return -1
}

// graphCursor is the row of the card's selected node.
func (m *Model) graphCursor() int {
	first, _, ok := m.peekSpan()
	if !ok {
		return m.cursor
	}

	_, line := layoutGraph(m.graphView(m.peeked.graph), m.cardMeasure(m.diffWidth()))

	return first + 1 + line
}

// callRows is the gc card: the title naming the function, the graph, and under
// it the call sites of the selected node where za opened them.
func (m *Model) callRows(p *peekCard) []row {
	g := p.graph.graph
	center := p.sites[0]
	card := func(text string) row { return row{kind: rowPeek, comment: noComment, text: text, site: center} }

	head := card(fmt.Sprintf("⇢ calls · %s · %s", g.Center.Name, siteWord(center)))
	head.head = true
	head.aside = plural(len(g.Callers), "caller") + " · " + plural(len(g.Callees), "callee")

	lines, _ := layoutGraph(m.graphView(p.graph), m.cardMeasure(m.diffWidth()))
	rows := []row{head}

	for _, l := range lines {
		r := card(l.text)
		r.lit = l.spans
		rows = append(rows, r)
	}

	if p.graph.open {
		rows = append(rows, m.callSiteRows(p.graph)...)
	}

	return framed(rows, row{kind: rowPeek, comment: noComment}, m.diffWidth())
}

func (m *Model) callSiteRows(c *callGraph) []row {
	n, ok := c.selected()
	if !ok || len(n.Sites) == 0 {
		return nil
	}

	title := "call sites · " + n.Sites[0].Path
	rows := []row{{
		kind: rowPeek, comment: noComment, text: title, site: n.Sites[0],
		lit: []highlight.Span{{From: 0, To: len(title), Class: highlight.Comment}},
	}}

	for _, s := range n.Sites[:min(len(n.Sites), graphSites)] {
		rows = append(rows, m.excerptRows(s, 1, graphSites)...)
	}

	if rest := len(n.Sites) - graphSites; rest > 0 {
		more := fmt.Sprintf("+%d more", rest)
		rows = append(rows, row{kind: rowPeek, comment: noComment, text: more, site: n.Sites[0]})
	}

	return rows
}

func (m *Model) graphView(c *callGraph) graphView {
	nodes := func(in []diag.CallNode) []graphNode {
		out := make([]graphNode, 0, len(in))
		for _, n := range in {
			out = append(out, m.graphNodeOf(n))
		}

		return out
	}

	center := m.graphNodeOf(c.graph.Center)
	center.times = 0

	return graphView{
		center: center, callers: nodes(c.graph.Callers), callees: nodes(c.graph.Callees), side: c.side, at: c.at,
		tops: c.tops,
	}
}

// graphNodeOf marks a function the change edits and one with an open finding in
// its lines, both read off the span of its definition.
func (m *Model) graphNodeOf(n diag.CallNode) graphNode {
	where, faint := whereOf(n)

	return graphNode{
		name: n.Name, times: len(n.Sites), where: where, faint: faint,
		edited: m.diff.Touches(n.Path, n.From, n.To),
		judge: m.toJudgeWhere(func(t *threads.Thread) bool {
			return t.Path == n.Path && t.Line >= n.From && t.Line <= n.To
		}) > 0,
	}
}

// whereOf is where a node lives: its file and line in the checkout, and the
// package the server names for one outside it.
func whereOf(n diag.CallNode) (string, bool) {
	site := siteWord(diag.Site{Path: n.Path, Line: n.Line})
	if !filepath.IsAbs(n.Path) {
		return site, false
	}

	if n.Detail != "" {
		return n.Detail, true
	}

	return site, true
}
