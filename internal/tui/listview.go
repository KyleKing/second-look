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
	b.WriteString(l.modes())
	b.WriteByte('\n')
	b.WriteString(l.bodyView())
	b.WriteByte('\n')
	b.WriteString(l.footer())

	frame := b.String()
	if l.help {
		frame = l.helpOver(frame)
	}

	if l.labeler != nil {
		frame = l.labelOver(frame)
	}

	return frame
}

func (l *List) header() string {
	left := l.styles.title.Render(l.title)

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
	}

	if l.focused != "" || l.filter.on() {
		right = l.styles.subtitle.Render(l.counted() + " ")
	}

	gap := l.width - textWidth(left) - textWidth(right)
	if right == "" || gap < 1 {
		return cut(left, l.width)
	}

	return left + strings.Repeat(" ", gap) + right
}

// modes is the header's second line, the review screen's own shape: the tab
// strip on the left and, on the right, what is narrowing the rows. The section
// yields to the filter and the strip, since it is the one part of the line a
// reader can recover by looking at the rows under it.
func (l *List) modes() string {
	var words []string

	if l.filter.on() {
		words = append(words, "/"+l.filter.query)
	}

	if section := l.section(); section != "" {
		words = append(words, section)
	}

	if len(words) == 0 {
		return cut(l.tabStrip(), l.width)
	}

	right := l.styles.hunk.Render(strings.Join(words, " · ") + " ")
	room := l.width - textWidth(right) - indent

	if room < 1 {
		return cut(right, l.width)
	}

	left := cut(l.tabStrip(), room)

	return left + strings.Repeat(" ", max(1, l.width-textWidth(left)-textWidth(right))) + right
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
	nat := l.natural()
	lay := metaLayout(nat, width)

	out := make([]string, 0, rows)

	for i := l.offset; i < len(l.lines) && len(out) < rows; i++ {
		text := l.line(i, lay, width, nat.urgent)
		if l.lit(i) {
			text = lightLine(text, l.width-1, l.styles.selection.GetBackground())
		}

		out = append(out, text)
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(alongside(out, bar, l.styles, l.width), "\n")
}

func (l *List) line(i int, lay table.Layout, width int, urgent bool) string {
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
		face := l.styles.subtitle
		if line.row.Dim {
			face = l.styles.behind
		}

		return " " + face.Render(cut("      "+l.row(line.row, lay), width))
	}

	// A row whose Under carries a title reads title-first: the one line a
	// queue is scanned by, cut at a fixed cap so the scan holds at any width.
	var text string
	if line.row.Under != "" {
		text = cut(line.row.Under, min(titleCap, width-markRoom-bool2int(urgent)))
	} else {
		text = cut(l.row(line.row, lay), width)
	}

	var mark string

	switch {
	case line.row.Urgent:
		mark = l.styles.priority.Render("!")
	case urgent:
		mark = " "
	}

	switch {
	case i == l.cursor:
		return l.styles.cursor.Render(cursorBar) + mark + text
	case line.row.Dim:
		return " " + mark + l.styles.behind.Render(text)
	}

	return " " + mark + text
}

// lit reports whether line i belongs to the row under the cursor: its title and
// the meta line riding under it.
func (l *List) lit(i int) bool {
	if i == l.cursor {
		return true
	}

	line := l.lines[i]

	return line.under && i > 0 && l.lines[i-1].row == line.row && i-1 == l.cursor
}

// lightLine paints text's background to width cells. A face inside text ends
// with a reset that would drop the background, so each reset restores it.
func lightLine(text string, width int, bg color.Color) string {
	face := lipgloss.NewStyle().Background(bg).Render("x")
	on, _, _ := strings.Cut(face, "x")

	if over := width - textWidth(text); over > 0 {
		text += strings.Repeat(" ", over)
	}

	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		text = strings.ReplaceAll(text, reset, reset+on)
	}

	return on + text + "\x1b[m"
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

		cells = append(cells, l.cell(r, c.Key, w))
	}

	if marker := lay.Marker(); marker != "" {
		cells = append(cells, marker)
	}

	b.WriteString(table.Join(cells))

	return strings.TrimRight(b.String(), " ")
}

// cell is one column of a row's meta line, padded to the width it was given.
func (l *List) cell(r *Row, key string, w int) string {
	switch key {
	case colLeft:
		return pad(cutTail(r.Left, w), w)
	case colMid:
		mid := pad(cut(r.Mid, w), w)
		if r.Tone != ToneOrdinary {
			mid = l.toneFace(r.Tone).Render(mid)
		}

		return mid
	case colAge:
		return pad(r.Age, w)
	case colCost:
		// The rating is right-aligned so the digits line up, and it keeps
		// its width on a row that has none: a queue re-sorting as ratings
		// land would otherwise slide every title sideways on each answer.
		return lpad(r.Cost, w)
	case colAdd:
		return signed(r.Added, "+", w-1)
	case colDel:
		return signed(r.Removed, "-", w-1)
	case colWho:
		return pad(cut(r.Who, w), w)
	case colWhy:
		return pad(cut(r.Why, w), w)
	case colTail:
		return pad(cut(r.Tail, w), w)
	case colRemote:
		return pad(l.toneFace(r.RemoteTone).Render(cut(r.Remote, w)), w)
	case colAgent:
		return pad(l.toneFace(r.AgentTone).Render(cut(r.Agent, w)), w)
	}

	return ""
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

	hints := make([]hint, len(pairs))
	for i, p := range pairs {
		hints[i] = hint{key: p[0], what: p[1], off: slices.Contains(idle, p[0])}
	}

	return cut(" "+dimLine(l.styles, hints, l.width), l.width)
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

func bool2int(b bool) int {
	if b {
		return 1
	}

	return 0
}
