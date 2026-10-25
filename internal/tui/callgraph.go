package tui

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/highlight"
)

// The call graph's grid. Every side draws at most graphSide rows and scrolls
// the nodes past them, and the three text columns have a ceiling and a floor
// they are squeezed between when the card is narrow.
const (
	graphSide         = 5
	graphFlags        = 3
	graphHalves       = 2
	markAndGap        = 2
	graphNameMax      = 22
	graphCenter       = 26
	graphWhereMin     = 12
	graphNameMin      = 6
	graphChrome       = 29
	graphCenterChrome = 11
	noCallers         = "no callers"
	noCallees         = "no callees"
	graphMark         = "›"
	graphEdited       = "✎"
	graphJudge        = "◆"
)

// The two sides a node can be on.
const (
	sideCallers = iota
	sideCallees
)

// graphNode is one function as the layout draws it. A rest node is the marker
// for the functions a side has scrolled out of view, and its where is their
// names.
type graphNode struct {
	name   string
	times  int
	edited bool
	judge  bool
	where  string
	faint  bool
	rest   bool
}

// graphView is everything the layout needs, with the selection as a side and an
// index into that side's nodes, and where each side is scrolled to.
type graphView struct {
	center  graphNode
	callers []graphNode
	callees []graphNode
	side    int
	at      int
	tops    [2]int
}

// graphLine is one row of the layout and the spans that color it, byte offsets
// into text.
type graphLine struct {
	text  string
	spans []highlight.Span
}

// graphCols are the widths of the grid's four text columns.
type graphCols struct{ left, center, right, where int }

func (c graphCols) total() int {
	t := c.left + c.center + c.right + graphChrome
	if c.where > 0 {
		t += c.where + 1
	}

	return t
}

// layoutGraph draws callers on the left, callees on the right and the function
// between them, and returns the line the selected node is on. Name, flag, and
// location columns are the same width on every line, so a column reads straight
// down however many nodes a side has.
func layoutGraph(v graphView, width int) ([]graphLine, int) {
	left, firstLeft := scrolled(v.callers, v.tops[sideCallers])
	right, firstRight := scrolled(v.callees, v.tops[sideCallees])
	mid := max(trunkRow(len(left)), trunkRow(len(right)))
	g := grid{
		center: v.center, mid: mid, left: newGridSide(left, mid), right: newGridSide(right, mid),
		cols: fitColumns(graphColumns(v.center, left, right), width),
		side: v.side,
	}

	selected := mid

	switch v.side {
	case sideCallers:
		g.at = v.at - v.tops[sideCallers] + firstLeft
		selected = g.left.rowOf(g.at)
	case sideCallees:
		g.at = v.at - v.tops[sideCallees] + firstRight
		selected = g.right.rowOf(g.at)
	}

	if g.at < 0 || selected < 0 {
		selected = mid
	}

	rows := max(mid, g.left.hi, g.right.hi) + 1
	out := make([]graphLine, 0, rows)

	for row := range rows {
		out = append(out, g.draw(row))
	}

	return out, selected
}

// trunkRow is how far down a side of k nodes the center's row falls: the middle
// node, the upper of the two where k is even.
func trunkRow(k int) int { return max(0, (k-1)/graphHalves) }

// gridSide places one side's nodes on consecutive rows, positioned so the
// center's row is one of them.
type gridSide struct {
	nodes  []graphNode
	byRow  map[int]int
	lo, hi int
}

func newGridSide(nodes []graphNode, mid int) gridSide {
	s := gridSide{nodes: nodes, byRow: map[int]int{}, lo: mid - trunkRow(len(nodes))}
	s.hi = s.lo + len(nodes) - 1

	for i := range nodes {
		s.byRow[s.lo+i] = i
	}

	return s
}

func (s gridSide) rowOf(i int) int { return s.lo + i }

// bracket is the vertical rule joining a side's nodes to the center, drawn from
// the first node's row to the last, which the center's row is always among.
func (s gridSide) bracket(row int, toward, away bool) string {
	if len(s.nodes) == 0 || row < s.lo || row > s.hi {
		return " "
	}

	return junction(toward, away, row > s.lo, row < s.hi)
}

type grid struct {
	center      graphNode
	left, right gridSide
	cols        graphCols
	mid         int
	side, at    int
}

// draw is one row of the grid, trimmed of the blanks the columns pad it with.
func (g grid) draw(row int) graphLine {
	var b lineBuilder

	li, hasL := g.left.byRow[row]
	ri, hasR := g.right.byRow[row]
	onMid := row == g.mid

	switch {
	case hasL:
		b.cell(leftText(g.left.nodes[li]), g.left.nodes[li], g.cols.left, g.side == sideCallers && g.at == li)
	case onMid && len(g.left.nodes) == 0:
		b.add(" "+column(noCallers, g.cols.left+graphFlags+1), highlight.Comment)
	default:
		b.add(strings.Repeat(" ", g.cols.left+graphFlags+markAndGap), highlight.Plain)
	}

	b.add(" "+arm(hasL)+g.left.bracket(row, hasL, onMid), highlight.Plain)

	if onMid {
		b.center(g.center, g.cols.center, len(g.left.nodes) > 0, len(g.right.nodes) > 0)
	} else {
		b.add(strings.Repeat(" ", g.cols.center+graphCenterChrome), highlight.Plain)
	}

	b.add(g.right.bracket(row, onMid, hasR)+arm(hasR)+" ", highlight.Plain)

	switch {
	case hasR:
		b.cell(label(g.right.nodes[ri]), g.right.nodes[ri], g.cols.right, g.side == sideCallees && g.at == ri)
		b.where(g.right.nodes[ri], g.cols.where)
	case onMid && len(g.right.nodes) == 0:
		b.add(" "+noCallees, highlight.Comment)
	}

	return b.line()
}

// scrollSpan is the nodes [top, to) an n-node side shows when scrolled to top,
// and whether a marker row stands in for the nodes above and for the nodes
// below. The markers take rows of the graphSide the side has, so the side is as
// tall at one scroll position as at any other and the center's row does not move.
type scrollSpan struct {
	to       int
	up, down bool
}

func window(n, top int) scrollSpan {
	room := graphSide
	if top > 0 {
		room--
	}

	if n-top <= room {
		return scrollSpan{to: n, up: top > 0}
	}

	return scrollSpan{to: top + room - 1, up: top > 0, down: true}
}

// scrollTo is the top that shows node at when the side was scrolled to top: the
// same one while it is in view, and just far enough otherwise.
func scrollTo(n, at, top int) int {
	top = min(top, at)

	for {
		if at < window(n, top).to {
			return top
		}

		top++
	}
}

// scrolled is the rows a side draws at top, markers included, and the row of
// the first node among them.
func scrolled(nodes []graphNode, top int) ([]graphNode, int) {
	w := window(len(nodes), top)
	rows := make([]graphNode, 0, graphSide)
	first := 0

	if w.up {
		rows, first = append(rows, hidden("↑", nodes[:top])), 1
	}

	rows = append(rows, nodes[top:w.to]...)

	if w.down {
		rows = append(rows, hidden("↓", nodes[w.to:]))
	}

	return rows, first
}

func hidden(arrow string, nodes []graphNode) graphNode {
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.name)
	}

	return graphNode{name: fmt.Sprintf("%s %d more", arrow, len(names)), where: strings.Join(names, ", "), rest: true}
}

func graphColumns(center graphNode, left, right []graphNode) graphCols {
	c := graphCols{center: min(textWidth(label(center)), graphCenter)}

	for _, n := range left {
		c.left = max(c.left, textWidth(leftText(n)))
	}

	for _, n := range right {
		c.right = max(c.right, textWidth(label(n)))
		c.where = max(c.where, textWidth(n.where))
	}

	c.left, c.right = min(c.left, graphNameMax), min(c.right, graphNameMax)

	if len(left) == 0 {
		c.left = len(noCallers)
	}

	if len(right) == 0 {
		c.right = len(noCallees)
	}

	return c
}

// fitColumns narrows the grid to width: the location column goes first, whole
// once it would be under its floor, and then the widest name column a cell at a
// time.
func fitColumns(c graphCols, width int) graphCols {
	for c.total() > width {
		switch {
		case c.where > 0:
			c.where -= c.total() - width
			if c.where < graphWhereMin {
				c.where = 0
			}
		case c.left >= c.right && c.left >= c.center && c.left > graphNameMin:
			c.left--
		case c.right >= c.center && c.right > graphNameMin:
			c.right--
		case c.center > graphNameMin:
			c.center--
		default:
			return c
		}
	}

	return c
}

// label is a node's name with how many call sites it stands for.
func label(n graphNode) string {
	if n.times > 1 {
		return fmt.Sprintf("%s ×%d", n.name, n.times)
	}

	return n.name
}

func leftText(n graphNode) string {
	if n.rest {
		return n.name + " " + n.where
	}

	return label(n)
}

func flags(n graphNode) string {
	out := []string{" ", " ", " "}
	if n.edited {
		out[0] = graphEdited
	}

	if n.judge {
		out[2] = graphJudge
	}

	return strings.Join(out, "")
}

func arm(on bool) string {
	if on {
		return "──"
	}

	return "  "
}

// junction is the box-drawing cell where the arms that leave it meet.
func junction(left, right, up, down bool) string {
	var at int

	for i, on := range []bool{left, right, up, down} {
		if on {
			at |= 1 << i
		}
	}

	return []string{" ", "╴", "╶", "─", "╵", "┘", "└", "┴", "╷", "┐", "┌", "┬", "│", "┤", "├", "┼"}[at]
}

// column cuts s to width cells and pads it out to them.
func column(s string, width int) string { return pad(cut(s, width), width) }

// lineBuilder writes a line and records where each classed run lands in it.
type lineBuilder struct {
	b     strings.Builder
	spans []highlight.Span
}

func (l *lineBuilder) add(s string, class highlight.Class) {
	from := l.b.Len()
	l.b.WriteString(s)

	if class != highlight.Plain && s != "" {
		l.spans = append(l.spans, highlight.Span{From: from, To: l.b.Len(), Class: class})
	}
}

// cell is a node's marker, name, and flags.
func (l *lineBuilder) cell(text string, n graphNode, width int, picked bool) {
	mark, class := " ", highlight.Plain

	switch {
	case n.rest:
		class = highlight.Comment
	case picked:
		mark, class = graphMark, highlight.Function
	}

	l.add(mark, highlight.Plain)
	l.add(column(text, width), class)
	l.add(" "+flags(n), highlight.Plain)
}

func (l *lineBuilder) center(n graphNode, width int, callers, callees bool) {
	lead, tail := "  ▶ ", "  "

	if callers {
		lead = "──▶ "
	}

	if callees {
		tail = "──"
	}

	l.add(lead, highlight.Plain)
	l.add(column(label(n), width), highlight.Type)
	l.add(" "+flags(n)+" "+tail, highlight.Plain)
}

func (l *lineBuilder) where(n graphNode, width int) {
	if width == 0 {
		return
	}

	class := highlight.Plain
	if n.faint || n.rest {
		class = highlight.Comment
	}

	l.add(" ", highlight.Plain)
	l.add(cutPath(n.where, width), class)
}

func (l *lineBuilder) line() graphLine {
	text := l.b.String()
	trimmed := strings.TrimRight(text, " ")

	spans := make([]highlight.Span, 0, len(l.spans))

	for _, s := range l.spans {
		if s.From < len(trimmed) {
			s.To = min(s.To, len(trimmed))
			spans = append(spans, s)
		}
	}

	return graphLine{text: trimmed, spans: spans}
}

// cutPath cuts a location to width, dropping leading directories before it
// touches the file name and line.
func cutPath(s string, width int) string {
	for textWidth(s) > width {
		i := strings.Index(strings.TrimPrefix(s, "…/"), "/")
		if i < 0 {
			return cutTail(s, width)
		}

		s = "…/" + strings.TrimPrefix(s, "…/")[i+1:]
	}

	return s
}
