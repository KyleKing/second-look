package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/table"
)

// Role says what a span of a grid cell is. The screen maps it to a face, so a
// caller names what it is drawing and never what this terminal draws it in.
type Role uint8

// The roles a grid cell is drawn in.
const (
	// RoleCell is ordinary column text: a ref, an author, a status word.
	RoleCell Role = iota
	// RoleText is the line a row is scanned by.
	RoleText
	// RoleQuiet is what frames the text: a type prefix, an owner, a connector.
	RoleQuiet
	RoleAge
	RolePriority
	RoleAdd
	RoleRemove
	// RolePending stands in for a fact that has not arrived.
	RolePending
	// The tones color a glyph.
	RoleGood
	RoleNew
	RoleWarn
	RoleBad
	RoleMuted
	RoleMerged
)

// Span is a run of text in one role.
type Span struct {
	Text string
	Role Role
}

// Cell is one column of a grid row. Long replaces Spans in a column LongAt
// cells wide or more, which is how a status gets a longer word where there is
// room for it.
type Cell struct {
	Spans  []Span
	Long   []Span
	LongAt int
}

// Pending is the cell of a fact that is not known yet. The column's alignment
// places it.
func Pending() Cell { return Cell{Spans: []Span{{Text: pendingMark, Role: RolePending}}} }

// pendingMark stands in for a fact not yet known, and gridMargin is what a grid
// row spends left of its first column.
const (
	pendingMark = "·"
	gridMargin  = 2
)

// role is the face a span draws in. A row that is waiting on somebody else
// recedes: its text steps down a rung and its sizes lose their colors.
func (s styles) role(r Role, waiting bool) lipgloss.Style {
	rungs := &s.rungs

	switch r {
	case RoleText:
		return rungIf(waiting, rungs.waiting, rungs.title).dim
	case RoleQuiet:
		return rungs.mark.dim
	case RoleAge:
		return rungs.age.dim
	case RolePriority:
		return rungs.priority.dim
	case RoleAdd:
		return rungIf(waiting, rungs.age, rungs.add).dim
	case RoleRemove:
		return rungIf(waiting, rungs.age, rungs.remove).dim
	case RolePending:
		return s.placeholder
	case RoleGood:
		return rungs.good.dim
	case RoleNew:
		return rungs.fresh.dim
	case RoleWarn:
		return rungs.warn.dim
	case RoleBad:
		return rungs.bad.dim
	case RoleMuted:
		return rungs.muted.dim
	case RoleMerged:
		return rungs.merged.dim
	case RoleCell:
	}

	return rungIf(waiting, rungs.waiting, rungs.cell).dim
}

func rungIf(first bool, a, b rung) rung {
	if first {
		return a
	}

	return b
}

// gridColumns is the tab's columns with each breakpoint read against the
// terminal's width, which is what a breakpoint is named in, rather than the
// narrower table left once the margin and scrollbar are spent.
func (l *List) gridColumns() []table.Column {
	cols := l.columns()

	for i := range cols {
		switch {
		case cols[i].From == 0:
		case cols[i].From <= l.width:
			cols[i].From = 0
		default:
			cols[i].From = l.width + 1
		}
	}

	return cols
}

// gridLine draws row i on the grid: the margin, then each column's cell at the
// width the layout gave it. A row under the cursor carries its background in
// every cell and gutter, so no reset inside the line ends the band early.
func (l *List) gridLine(i int, lay table.Layout) string {
	row := l.lines[i].row
	face := lipgloss.NewStyle()
	margin := strings.Repeat(" ", gridMargin)

	lit := i == l.cursor
	if lit {
		face = face.Background(l.styles.selection.GetBackground())
		margin = l.styles.cursor.Render(cursorBar) + face.Render(" ")
	}

	var b strings.Builder

	b.WriteString(margin)

	gutter := face.Render(strings.Repeat(" ", table.Gutter))

	for n, c := range lay.Columns {
		if n > 0 {
			b.WriteString(gutter)
		}

		w := lay.Width(c.Key)
		cell := row.Cells[c.Key]

		spans := cell.Spans
		if cell.Long != nil && w >= cell.LongAt {
			spans = cell.Long
		}

		b.WriteString(l.fit(spans, c, w, row.Dim, face))
	}

	if over := l.width - 1 - gridMargin - lay.Total(); lit && over > 0 {
		b.WriteString(face.Render(strings.Repeat(" ", over)))
	}

	return b.String()
}

// fit draws spans in exactly w cells. Text that fits is padded on the side the
// column's alignment leaves free. Text that does not is cut as a whole, and a
// cell of several spans keeps each span's face up to the cut.
func (l *List) fit(spans []Span, c table.Column, w int, waiting bool, face lipgloss.Style) string {
	var plain strings.Builder
	for _, s := range spans {
		plain.WriteString(s.Text)
	}

	paint := func(text string, r Role) string {
		if text == "" {
			return ""
		}

		return face.Inherit(l.styles.role(r, waiting)).Render(text)
	}

	blank := func(n int) string { return paint(strings.Repeat(" ", n), RoleCell) }

	have := textWidth(plain.String())
	if have <= w {
		var b strings.Builder

		if c.Align == table.AlignRight {
			b.WriteString(blank(w - have))
		}

		for _, s := range spans {
			b.WriteString(paint(s.Text, s.Role))
		}

		if c.Align != table.AlignRight {
			b.WriteString(blank(w - have))
		}

		return b.String()
	}

	cut := table.PadTrim(plain.String(), w, c.Align, c.Trim)

	if c.Trim == table.TrimLeft || len(spans) < 2 {
		return paint(cut, spans[len(spans)-1].Role)
	}

	var b strings.Builder

	for _, s := range spans {
		if !strings.HasPrefix(cut, s.Text) {
			b.WriteString(paint(cut, s.Role))

			return b.String()
		}

		b.WriteString(paint(s.Text, s.Role))
		cut = cut[len(s.Text):]
	}

	return b.String()
}
