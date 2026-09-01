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
	b.WriteString(l.bodyView())
	b.WriteByte('\n')
	b.WriteString(l.footer())

	return b.String()
}

func (l *List) header() string {
	left := l.styles.title.Render(" " + l.title)

	if l.subtitle == nil {
		return left
	}

	right := l.styles.subtitle.Render(l.subtitle() + " ")

	// The section yields to the counts and the title, since it is the one part
	// of the line a reader can recover by looking at the rows under it.
	const shortest = 8

	room := l.width - textWidth(left) - textWidth(right) - indent
	if section := l.section(); section != "" && room > shortest {
		left += l.styles.subtitle.Render("  " + cut(section, room))
	}

	gap := l.width - textWidth(left) - textWidth(right)
	if gap < 1 {
		return cut(left, l.width)
	}

	return left + strings.Repeat(" ", gap) + right
}

// section is the heading the cursor sits under, which the header carries
// because the heading itself scrolls away exactly when the list is long enough
// to need it.
func (l *List) section() string {
	for i := min(l.cursor, len(l.lines)-1); i >= 0; i-- {
		if h := l.lines[i].heading; h != "" {
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

	left, mid := l.widest()

	out := make([]string, 0, rows)

	for i := l.offset; i < len(l.lines) && len(out) < rows; i++ {
		out = append(out, l.line(i, left, mid))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(out, "\n")
}

func (l *List) line(i, left, mid int) string {
	line := l.lines[i]

	switch {
	case line.heading != "":
		return " " + l.styles.file.Render(" "+line.heading)
	case line.detail != "":
		return " " + l.styles.body.Render(cut("        "+line.detail, l.width-1))
	case line.under:
		return " " + l.styles.note.Render(cut("      "+line.Under(), l.width-1))
	}

	text := cut(l.row(line.row, left, mid), l.width-1)

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
func (l *List) row(r *Row, left, mid int) string {
	var b strings.Builder

	b.WriteString(" ")

	if r.Unread {
		b.WriteString(l.styles.rail.Render("●"))
	} else {
		b.WriteString(" ")
	}

	b.WriteString(" " + pad(cutTail(r.Left, left), left))
	b.WriteString("  " + pad(cut(r.Mid, mid), mid))
	// The age column is fixed rather than measured: "13h" and "4d" are the whole
	// range, and letting it vary would move the columns beside it between lists.
	const ageWidth = 5

	b.WriteString("  " + pad(r.Age, ageWidth))

	if r.Tail != "" {
		b.WriteString("  " + r.Tail)
	}

	return b.String()
}

func (l *List) footer() string {
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
			{"space", "mark read"},
			{"r", "reply"},
			{"R", "resolve"},
			{"o", "GitHub"},
			{"tab", "group"},
			{"?", "help"},
			{"q", quitWord},
		}
	}

	return cut(" "+hintLine(l.styles, hints), l.width)
}

func (l *List) helpView() string {
	if l.helpLines != nil {
		return strings.Join(append([]string{l.title, ""}, l.helpLines...), "\n")
	}

	lines := []string{
		l.title,
		"",
		"  j/k, ctrl+u/d, g/G   move, half page, top and bottom",
		"  tab                  the next group",
		"  enter                read the whole conversation, and mark it read",
		"  space                mark read without opening it",
		"  r                    reply, staged into the prepared review",
		"  R                    resolve the thread, or thumbs-up what cannot be resolved",
		"  o                    open it on GitHub",
		"  ctrl+r               read the queue again",
		"  q, esc               leave",
		"",
		"  ● marks a conversation that moved since you last read it.",
	}

	return strings.Join(lines, "\n")
}
