package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/frame"
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
	v.MouseMode = tea.MouseModeCellMotion

	return v
}

func (l *List) render() string {
	if l.width < minWidth || l.height < minHeight {
		return "the terminal is too small: " +
			strconv.Itoa(minWidth) + "x" + strconv.Itoa(minHeight) + " is the minimum"
	}

	f := l.frame()
	f.Body = strings.Split(l.bodyView(), "\n")
	f.Panel = l.inspector()
	f.Hints = l.footerHints()

	lines := strings.Split(frame.Render(l.styles.skin, f, l.width, l.height), "\n")
	if over := l.footerOver(); over != "" {
		lines[len(lines)-1] = frame.Lit(over, l.width, l.styles.panel)
	}

	if l.help {
		lines = l.helpOver(lines)
	}

	out := strings.Join(lines, "\n")
	if l.labeler != nil {
		out = l.labelOver(out)
	}

	return marginate(out, l.padX, l.padY)
}

// frame is the chrome around the rows: the program's name, the tabs, on the
// right what changes how to read the list, and on the rule under them what is
// narrowing the rows. Its Panel holds the inspector's height, so the geometry a
// click is read against is the one drawn.
func (l *List) frame() frame.Frame {
	return frame.Frame{
		Title: l.title,
		Strip: true,
		Tabs:  l.frameTabs(),
		Facts: l.facts(),
		Note:  l.note(),
		Panel: make([]string, l.inspectorRows()),
	}
}

// facts is the header's right side. A narrowing filter or a focused repository
// takes it, since a queue that is quiet for the wrong reason is the worst thing
// narrowing it can do.
func (l *List) facts() []frame.Fact {
	if l.focused != "" || l.filter.on() {
		var out []frame.Fact

		if l.focused != "" {
			out = append(out, frame.Fact{Text: l.focused, Tone: frame.Accent})

			if l.focusNote != "" {
				out = append(out, frame.Fact{Text: l.focusNote})
			}
		}

		return append(out, frame.Fact{Text: l.counted()})
	}

	if l.state == nil {
		return nil
	}

	s := l.state()
	if s.Text == "" {
		return nil
	}

	tone := frame.Muted
	if s.Busy {
		tone = frame.Warning
	}

	return []frame.Fact{{Text: s.Text, Tone: tone}}
}

// note is the rule's right end: the filter, then the heading scrolled past. The
// section yields to the filter, since it is the part a reader can recover by
// looking at the rows under it.
func (l *List) note() string {
	var words []string

	if l.filter.on() {
		words = append(words, "/"+l.filter.query)
	}

	if section := l.section(); section != "" {
		words = append(words, section)
	}

	return strings.Join(words, " · ")
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

// footerOver is what takes the footer's whole line instead of the keys: the
// label picker, the filter being typed, or a status message.
func (l *List) footerOver() string {
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

		status := l.status
		if l.spinning {
			status = spinFrames[l.spin%len(spinFrames)] + " " + status
		}

		return style.Render(cut(status, l.width))
	}

	return ""
}

func (l *List) footerHints() []keyhint.Hint {
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

	return hints
}

// helpOver floats the legend over the list's bottom corner, the same window
// the review screen's legend and a waiting chord draw. A key anywhere closes
// it, so it borrows the screen rather than replacing it.
func (l *List) helpOver(lines []string) []string {
	hints := l.helpLines
	if hints == nil {
		hints = defaultListHelp()
	}

	rows := keyhint.Column(legendStyles(l.styles), asHints(hints), helpWidth(l.width))
	inner := min(len(rows), l.height-boxBorder-1)
	l.helpAt = min(l.helpAt, max(0, len(rows)-inner))
	box := overlay.Box(boxStyles(l.styles), "keys", rows, inner, l.helpAt)

	x, y := l.width-textWidth(box[0])-1, max(0, l.height-len(box)-1)
	l.helpBox = overlay.At(box, x, y)

	return overlay.Blit(lines, box, x, y)
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
