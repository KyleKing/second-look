package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/overlay"
	"github.com/kyleking/aragonite/tui/table"
)

// View draws the list. It is the same three bands as the review screen -- a
// header, the body, a footer -- so moving between the two screens does not
// move where anything is.
func (l *List) View() tea.View {
	v := tea.NewView(l.render())
	v.AltScreen = true
	if l.help {
		v.MouseMode = tea.MouseModeCellMotion
	}

	return v
}

func (l *List) render() string {
	if l.width < minWidth || l.height < minHeight {
		return "the terminal is too small: " +
			strconv.Itoa(minWidth) + "x" + strconv.Itoa(minHeight) + " is the minimum"
	}

	var b strings.Builder

	b.WriteString(l.header())
	b.WriteByte('\n')

	if strip := l.tabStrip(); strip != "" {
		b.WriteString(" " + strip)
		b.WriteByte('\n')
	}

	b.WriteString(l.bodyView())
	b.WriteByte('\n')
	b.WriteString(l.footer())

	frame := b.String()
	if l.help {
		frame = l.helpOver(frame)
	}

	return frame
}

func (l *List) header() string {
	left := l.styles.title.Render(" " + l.title)

	right := ""
	if l.subtitle != nil {
		right = l.styles.subtitle.Render(l.subtitle() + " ")
	}

	// What is being held back outranks the counts, since a queue that is quiet
	// for the wrong reason is the worst thing narrowing it can do.
	if l.focused != "" {
		left += l.styles.file.Render("  " + l.focused)

		if l.focusNote != "" {
			left += l.styles.subtitle.Render("  " + l.focusNote)
		}

		right = l.styles.subtitle.Render(l.counted() + " ")
	}

	if l.filter.on() {
		right = l.styles.subtitle.Render("/" + l.filter.query + "  " + l.counted() + " ")
	}

	// The section yields to the counts and the title, since it is the one part
	// of the line a reader can recover by looking at the rows under it.
	const shortest = 8

	room := l.width - textWidth(left) - textWidth(right) - indent
	if section := l.section(); section != "" && room > shortest {
		left += l.styles.subtitle.Render("  " + cut(section, room))
	}

	gap := l.width - textWidth(left) - textWidth(right)
	if right == "" || gap < 1 {
		return cut(left, l.width)
	}

	return left + strings.Repeat(" ", gap) + right
}

// section is the heading the cursor sits under, and only once that heading has
// scrolled off the top: a list long enough to lose it is the one that needs it,
// and naming a heading the reader can already see wastes the line.
func (l *List) section() string {
	for i := min(l.cursor, len(l.lines)-1); i >= 0; i-- {
		if h := l.lines[i].heading; h != "" {
			if i >= l.offset {
				return ""
			}

			return h
		}
	}

	return ""
}

func (l *List) bodyView() string {
	rows := l.visible()
	if rows <= 0 || len(l.lines) == 0 {
		return l.styles.subtitle.Render("  nothing here")
	}

	bar := scrollbar(rows, len(l.lines), l.offset)
	width := bodyWidth(l.width, bar)
	lay := metaLayout(l.natural(), width)

	out := make([]string, 0, rows)

	for i := l.offset; i < len(l.lines) && len(out) < rows; i++ {
		out = append(out, l.line(i, lay, width))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(alongside(out, bar, l.styles, l.width), "\n")
}

func (l *List) line(i int, lay table.Layout, width int) string {
	line := l.lines[i]

	switch {
	case line.heading != "":
		return " " + l.styles.file.Render(" "+line.heading)
	case line.detail != "":
		return " " + l.styles.body.Render(cut("        "+line.detail, width))
	case line.under:
		// The columns ride under the title as the row's meta line, indented to
		// one depth so it always starts in the same column: the title is what
		// a row is for, and the numbers are read after it or not at all.
		return " " + l.styles.subtitle.Render(cut("      "+l.row(line.row, lay), width))
	}

	// A row whose Under carries a title reads title-first: the one line a
	// queue is scanned by, cut at a fixed cap so the scan holds at any width.
	var text string
	if line.row.Under != "" {
		text = cut(line.row.Under, min(titleCap, width-markRoom))
	} else {
		text = cut(l.row(line.row, lay), width)
	}

	if i == l.cursor {
		return l.styles.cursor.Render(cursorBar) + text
	}

	return " " + text
}

// titleCap is how far right a row's title may run before it is cut: one line,
// a fixed end to scan down, and the rest of it a keystroke away. MarkRoom is
// the cell the cursor bar (or its absence) spends ahead of it.
const (
	titleCap = 80
	markRoom = 2
)

// row lays out one row's meta: the mark, then the columns the layout resolved.
// The mark comes first because unread is the one thing scanned for, and a glyph
// carries it so it survives NO_COLOR. Every field is its own cell, so a word a
// row does not carry leaves a blank rather than shifting the words after it.
func (l *List) row(r *Row, lay table.Layout) string {
	var b strings.Builder

	b.WriteString(" ")

	if r.Unread {
		b.WriteString(l.toneFace(r.Tone).Render("●"))
	} else {
		b.WriteString(" ")
	}

	b.WriteString(" ")

	cells := make([]string, 0, len(lay.Columns)+1)

	for _, c := range lay.Columns {
		w := lay.Width(c.Key)

		switch c.Key {
		case colLeft:
			cells = append(cells, pad(cutTail(r.Left, w), w))
		case colMid:
			mid := pad(cut(r.Mid, w), w)
			if r.Tone != ToneOrdinary {
				mid = l.toneFace(r.Tone).Render(mid)
			}

			cells = append(cells, mid)
		case colAge:
			cells = append(cells, pad(r.Age, w))
		case colCost:
			// The rating is right-aligned so the digits line up, and it keeps
			// its width on a row that has none: a queue re-sorting as ratings
			// land would otherwise slide every title sideways on each answer.
			cells = append(cells, lpad(r.Cost, w))
		case colAdd:
			cells = append(cells, signed(r.Added, "+", w-1))
		case colDel:
			cells = append(cells, signed(r.Removed, "-", w-1))
		case colTail:
			cells = append(cells, pad(cut(r.Tail, w), w))
		case colRemote:
			cells = append(cells, pad(l.toneFace(r.RemoteTone).Render(cut(r.Remote, w)), w))
		case colAgent:
			cells = append(cells, pad(l.toneFace(r.AgentTone).Render(cut(r.Agent, w)), w))
		}
	}

	if marker := lay.Marker(); marker != "" {
		cells = append(cells, marker)
	}

	b.WriteString(table.Join(cells))

	return strings.TrimRight(b.String(), " ")
}

// toneFace is the face a word is drawn at, with the ordinary tone taking the
// accent, which is the mark's own color.
func (l *List) toneFace(t Tone) lipgloss.Style {
	switch t {
	case ToneGood:
		return l.styles.ok
	case ToneWarn:
		return l.styles.warn
	case ToneBad:
		return l.styles.fail
	case ToneMuted:
		return l.styles.hunk
	case ToneMerged:
		return l.styles.merged
	default:
		return l.styles.rail
	}
}

// signed draws one side of a row's size: the sign in a column of its own so the
// signs line up, and the count right-aligned after it so the digits do. A row
// nothing measured keeps the width and draws neither.
func signed(count, sign string, width int) string {
	if count == "" {
		return strings.Repeat(" ", width+1)
	}

	return sign + lpad(count, width)
}

func (l *List) footer() string {
	if l.filter.typing {
		return l.prompt()
	}

	if l.status != "" {
		style := l.styles.ok
		if l.failed {
			style = l.styles.fail
		}

		return style.Render(cut(" "+l.status, l.width))
	}

	hints := l.hints
	if hints == nil {
		hints = [][2]string{
			{"enter", "read"},
			{spaceKey, "mark read"},
			{"r", replyWord},
			{"R", "resolve"},
			{"o", "GitHub"},
			{tabKey, "group"},
			{"/", "filter"},
			{"?", "help"},
			{"q", quitWord},
		}
	}

	return cut(" "+hintLine(l.styles, hints), l.width)
}

// helpOver floats the legend over the list's bottom corner, the same window
// the review screen's legend and a waiting chord draw. A key anywhere closes
// it, so it borrows the screen rather than replacing it.
func (l *List) helpOver(frame string) string {
	hints := l.helpLines
	if hints == nil {
		hints = defaultListHelp()
	}

	const listBoxW = 60

	rows := keyhint.Column(legendStyles(l.styles), asHints(hints), min(listBoxW, l.width-boxMargin))
	inner := min(len(rows), l.height-boxBorder-1)
	l.helpAt = min(l.helpAt, max(0, len(rows)-inner))
	box := overlay.Box(boxStyles(l.styles), "keys", rows, inner, l.helpAt)

	lines := strings.Split(frame, "\n")
	for len(lines) < l.height {
		lines = append(lines, "")
	}

	y := max(0, l.height-len(box)-1)

	return strings.Join(overlay.Blit(lines[:l.height], box, l.width-textWidth(box[0])-1, y), "\n")
}

func defaultListHelp() [][2]string {
	return [][2]string{
		{"j/k, ctrl+u/d, g/G", "move, half page, top and bottom"},
		{"ctrl+e/ctrl+y", "scroll without moving the cursor"},
		{"1/2/3, ] / [", "the queue to read"},
		{tabKey, "the next group"},
		{"/", "narrow to the rows carrying a word; esc puts them back"},
		{"enter", "read the whole conversation, and mark it read"},
		{spaceKey, "mark read without opening it"},
		{"r", "reply, staged into the prepared review"},
		{"R", "resolve the thread, or thumbs-up what cannot be resolved"},
		{"o", "open it on GitHub"},
		{refreshKey, "read the queue again"},
		{"q, esc", "leave"},
		{},
		{"", "● marks a conversation that moved since you last read it."},
	}
}
