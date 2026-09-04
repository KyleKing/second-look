package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// View draws the list. It is the same three bands as the review screen -- a
// header, the body, a footer -- so moving between the two screens does not
// move where anything is.
func (l *List) View() tea.View {
	v := tea.NewView(l.render())
	v.AltScreen = true

	return v
}

func (l *List) render() string {
	if l.width < minWidth || l.height < minHeight {
		return "the terminal is too small: " +
			strconv.Itoa(minWidth) + "x" + strconv.Itoa(minHeight) + " is the minimum"
	}

	if l.help {
		return l.helpView()
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

	return b.String()
}

func (l *List) header() string {
	left := l.styles.title.Render(" " + l.title)

	right := ""
	if l.subtitle != nil {
		right = l.styles.subtitle.Render(l.subtitle() + " ")
	}

	// What the filter is holding back outranks the counts, since a queue that
	// is quiet for the wrong reason is the worst thing a filter can do.
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

	cols := l.widest()
	bar := scrollbar(rows, len(l.lines), l.offset)
	width := bodyWidth(l.width, bar)

	out := make([]string, 0, rows)

	for i := l.offset; i < len(l.lines) && len(out) < rows; i++ {
		out = append(out, l.line(i, cols, width))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(alongside(out, bar, l.styles, l.width), "\n")
}

func (l *List) line(i int, cols columns, width int) string {
	line := l.lines[i]

	switch {
	case line.heading != "":
		return " " + l.styles.file.Render(" "+line.heading)
	case line.detail != "":
		return " " + l.styles.body.Render(cut("        "+line.detail, width))
	case line.under:
		return " " + l.styles.note.Render(cut("      "+line.Under(), width))
	}

	text := cut(l.row(line.row, cols), width)

	if i == l.cursor {
		return l.styles.cursor.Render(cursorBar) + text
	}

	return " " + text
}

// Under is the quoted line under a row, kept as a method so the renderer reads
// the same way for every kind of line.
func (l listLine) Under() string {
	if l.row == nil {
		return ""
	}

	return l.row.Under
}

// row lays out one row: the mark, where it is, what it anchors to, how stale,
// then whose turn and the title. The mark comes first because unread is the one
// thing scanned for, and a glyph carries it so it survives NO_COLOR.
func (l *List) row(r *Row, cols columns) string {
	var b strings.Builder

	b.WriteString(" ")

	if r.Unread {
		b.WriteString(l.styles.rail.Render("●"))
	} else {
		b.WriteString(" ")
	}

	b.WriteString(" " + pad(cutTail(r.Left, cols.left), cols.left))
	b.WriteString("  " + pad(cut(r.Mid, cols.mid), cols.mid))
	// The age column is fixed rather than measured: "13h" and "4d" are the whole
	// range, and letting it vary would move the columns beside it between lists.
	const ageWidth = 5

	b.WriteString("  " + pad(r.Age, ageWidth))

	// The rating is right-aligned so the digits line up, and it keeps its width
	// on a row that has none: a queue re-sorting as ratings land would otherwise
	// slide every title sideways on each answer.
	if cols.cost > 0 {
		b.WriteString("  " + lpad(r.Cost, cols.cost))
	}

	if cols.added > 0 || cols.removed > 0 {
		b.WriteString("  " + signed(r.Added, "+", cols.added) + " " + signed(r.Removed, "-", cols.removed))
	}

	if r.Tail != "" {
		b.WriteString("  " + r.Tail)
	}

	return b.String()
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
			{"r", "reply"},
			{"R", "resolve"},
			{"o", "GitHub"},
			{"tab", "group"},
			{"/", "filter"},
			{"?", "help"},
			{"q", quitWord},
		}
	}

	return cut(" "+hintLine(l.styles, hints), l.width)
}

// helpView is the legend, drawn the same way the review screen's is: the keys
// right-aligned in one column, so a reader scans one edge rather than two.
func (l *List) helpView() string {
	hints := l.helpLines
	if hints == nil {
		hints = defaultListHelp()
	}

	return strings.Join(append(
		[]string{l.styles.title.Render(" " + l.title), ""},
		helpBlock(l.styles, hints, l.width)...,
	), "\n")
}

func defaultListHelp() [][2]string {
	return [][2]string{
		{"j/k, ctrl+u/d, g/G", "move, half page, top and bottom"},
		{"ctrl+e/ctrl+y", "scroll without moving the cursor"},
		{"1/2/3, ] / [", "the queue to read"},
		{"tab", "the next group"},
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
