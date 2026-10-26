package tui

import (
	"image/color"
	"slices"
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
	b.WriteString(l.rule())
	b.WriteByte('\n')
	b.WriteString(l.bodyView())
	b.WriteByte('\n')
	b.WriteString(l.panel())

	frame := b.String()
	if l.help {
		frame = l.helpOver(frame)
	}

	if l.labeler != nil {
		frame = l.labelOver(frame)
	}

	return frame
}

// header is the first of the header's two lines: the program's name, the tabs,
// and on the right what changes how to read the list. A narrowing filter or a
// focused repository takes the right side, since a queue that is quiet for the
// wrong reason is the worst thing narrowing it can do.
func (l *List) header() string {
	left, _ := l.lead()
	right := l.right()

	gap := l.width - textWidth(left) - textWidth(right)
	if right == "" || gap < 1 {
		return cut(left, l.width)
	}

	return left + strings.Repeat(" ", gap) + right
}

// lead is the header's left side and the cells the active tab spans in it.
func (l *List) lead() (string, stretch) {
	text := " " + l.styles.title.Render(l.title)

	strip, at := l.tabStrip()
	if strip == "" {
		return text, stretch{}
	}

	offset := textWidth(text) + len(tabGap)

	return text + tabGap + strip, stretch{from: offset + at.from, to: offset + at.to}
}

func (l *List) right() string {
	if l.focused != "" || l.filter.on() {
		var parts []string

		if l.focused != "" {
			parts = append(parts, l.styles.file.Render(l.focused))

			if l.focusNote != "" {
				parts = append(parts, l.styles.subtitle.Render(l.focusNote))
			}
		}

		parts = append(parts, l.styles.subtitle.Render(l.counted()))

		return strings.Join(parts, "  ") + " "
	}

	if l.state == nil {
		return ""
	}

	s := l.state()
	if s.Text == "" {
		return ""
	}

	face := l.styles.subtitle
	if s.Busy {
		face = l.styles.warn
	}

	return face.Render(s.Text + " ")
}

// ruleGap is the cells kept between the underline and the words on its right.
const ruleGap = 2

// rule is the header's second line: a hairline with the active tab marked by a
// heavier line under it, and on the right what is narrowing the rows. The
// section yields to the filter, since it is the one part of the line a reader
// can recover by looking at the rows under it.
func (l *List) rule() string {
	var words []string

	if l.filter.on() {
		words = append(words, "/"+l.filter.query)
	}

	if section := l.section(); section != "" {
		words = append(words, section)
	}

	_, under := l.lead()

	// The rule never runs under the words, so what is cut is the line.
	tail := ""
	room := l.width

	if len(words) > 0 {
		tail = " " + l.styles.hunk.Render(cut(strings.Join(words, " · "), max(1, l.width-under.to-ruleGap))) + " "
		room = l.width - textWidth(tail)
	}

	var b strings.Builder

	edge := min(under.from, room)
	b.WriteString(l.styles.hairline.Render(strings.Repeat("─", edge)))

	heavy := max(0, min(under.to, room)-edge)
	b.WriteString(l.styles.rail.Render(strings.Repeat("━", heavy)))
	b.WriteString(l.styles.hairline.Render(strings.Repeat("─", max(0, room-edge-heavy))))
	b.WriteString(tail)

	return b.String()
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
	grid := l.gridLayout(width)

	// The band runs to the last cell the scrollbar leaves.
	reach := l.width
	if bar != nil {
		reach -= trackWidth
	}

	out := make([]string, 0, rows)

	for i := l.offset; i < len(l.lines) && len(out) < rows; i++ {
		out = append(out, l.line(i, grid, width, reach))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(alongside(out, bar, l.styles, l.width), "\n")
}

// line draws line i: a lane's rule, a blank, a line of an expanded
// conversation, or a row on the grid.
func (l *List) line(i int, grid table.Layout, width, reach int) string {
	line := l.lines[i]

	switch {
	case line.blank:
		return ""
	case line.section != nil:
		return l.laneRule(line.section, width)
	case line.detail != "":
		return " " + l.styles.body.Render(cut("        "+line.detail, width))
	}

	return l.gridLine(i, grid, reach)
}

// laneRule is the line a group starts with: its name and its count. A group
// that needs action reads bold, and one waiting on somebody else reads quiet.
func (l *List) laneRule(s *Section, width int) string {
	faces := l.styles.lanes
	label, count := faces.label, faces.count

	if s.Recedes {
		label, count = faces.quietLabel, faces.quietCount
	}

	return cut("  "+label.Render(s.Name)+"  "+count.Render(laneCount(s)), width)
}

// laneCount is what stands beside a group's name: the note where the group
// cannot count its rows yet, else the count, with the key that shows a folded one.
func laneCount(s *Section) string {
	switch {
	case s.Note != "":
		return s.Note
	case s.Fold:
		return strconv.Itoa(len(s.Rows)) + " · z shows"
	}

	return strconv.Itoa(len(s.Rows))
}

// lightLine paints text's background to width cells.
func lightLine(text string, width int, bg color.Color) string {
	return litLine(text, width, lipgloss.NewStyle().Background(bg))
}

// litLine draws text to width cells in face. A face inside text ends with a
// reset that would drop it, so each reset restores it.
func litLine(text string, width int, face lipgloss.Style) string {
	on, _, _ := strings.Cut(face.Render("x"), "x")

	if over := width - textWidth(text); over > 0 {
		text += strings.Repeat(" ", over)
	}

	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		text = strings.ReplaceAll(text, reset, reset+on)
	}

	return on + text + "\x1b[m"
}

func (l *List) footer() string {
	if l.labeler != nil {
		return l.labeler.prompt(l.styles, l.width)
	}

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

	pairs := l.hints
	if pairs == nil {
		pairs = [][2]string{
			{enterKey, "read"},
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

	var idle []string
	if l.inert != nil {
		idle = l.inert(l.current())
	}

	hints := make([]keyhint.Hint, len(pairs))
	for i, p := range pairs {
		hints[i] = keyhint.Hint{
			Key: p[0], What: p[1], Off: slices.Contains(idle, p[0]), Priority: hintPriority(p[0]),
		}
	}

	return " " + keyhint.Fit(footerStyles(l.styles), hints, l.width-1)
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
		{enterKey, "read the whole conversation, and mark it read"},
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
