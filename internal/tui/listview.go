package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/empty"
	"github.com/kyleking/aragonite/tui/frame"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/palette"
	"github.com/kyleking/aragonite/tui/preview"
	uisplit "github.com/kyleking/aragonite/tui/split"
	"github.com/kyleking/aragonite/tui/table"
)

// previewSpec is the side preview's geometry: open beside the rows once the
// terminal has room, with a cell of air around the rule.
var previewSpec = uisplit.Spec{SideMin: sidePreviewMin, MainMax: sideGridMax, Pad: 1}

const (
	// The queue draws its preview beside the rows from this width.
	sidePreviewMin = 120
	// The grid keeps growing no wider than this beside the pane.
	sideGridMax = 96
)

// sidePreview reports whether the detail pane is open beside the rows.
func (l *List) sidePreview() bool {
	return l.width >= previewSpec.SideMin
}

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
	if l.pal.IsOpen() && l.pal.Mode() == palette.Browse {
		lines[len(lines)-1] = frame.Lit(iconsLine(l.styles, iconsResolution(), l.width-1), l.width, l.styles.panel)
	} else if over := l.footerOver(); over != "" {
		lines[len(lines)-1] = frame.Lit(over, l.width, l.styles.panel)
	} else if line := l.coachFooter(); line != "" {
		lines[len(lines)-1] = frame.Lit(line, l.width, l.styles.panel)
	}

	lines = l.overBody(lines, f)

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
	if rows <= 0 {
		return l.styles.subtitle.Render("  nothing here")
	}

	if len(l.lines) == 0 {
		return strings.Join(empty.Card(l.styles.skin, l.emptyCard(), l.width, rows), "\n")
	}

	if !l.sidePreview() {
		return strings.Join(l.bodyLines(rows, l.width), "\n")
	}

	// The rows are laid out at the width the split hands the main block, so a
	// column at the right end is fitted rather than cut by the preview's rule.
	return strings.Join(previewSpec.Render(
		uisplit.Side, uisplit.Styles{Rule: l.styles.skin.Edge}, l.width, rows,
		func(w, h int) []string { return preview.Pad(l.bodyLines(h, w), h, w) },
		l.previewPane,
	), "\n")
}

// emptyCard is what an empty queue says in place of its rows: what is empty,
// why, and the keys out, so a blank list never reads as a broken one.
func (l *List) emptyCard() empty.Empty {
	name := "this queue"
	if l.at >= 0 && l.at < len(l.tabs) && l.tabs[l.at].Name != "" {
		name = l.tabs[l.at].Name
	}

	if q := l.filter.query; q != "" {
		return empty.Empty{
			Title: "Nothing in " + name + " matches " + q,
			Why:   "The filter keeps the rows carrying every word typed; ! drops a word's matches instead.",
			Hints: []keyhint.Hint{{Key: "/", What: "change it"}, {Key: escKey, What: "clear it"}},
		}
	}

	hints := []keyhint.Hint{{Key: "?", What: "keys"}, {Key: "q", What: quitWord}}
	if len(l.tabs) > 1 {
		hints = append([]keyhint.Hint{{Key: "]", What: "next queue"}}, hints...)
	}

	return empty.Empty{
		Title: "Nothing in " + name,
		Why:   "No pull request or conversation is waiting here right now.",
		Hints: hints,
	}
}

// bodyLines is the list's rows laid out at width, with the scrollbar on the
// right edge when the rows overflow.
func (l *List) bodyLines(rows, width int) []string {
	bar := scrollbar(rows, len(l.lines), l.offset, l.styles)
	grid := l.gridLayout(bodyWidth(width, bar))

	// The band runs to the last cell the scrollbar leaves.
	reach := width
	if bar != nil {
		reach -= trackWidth
	}

	out := make([]string, 0, rows)

	for i := l.offset; i < len(l.lines) && len(out) < rows; i++ {
		out = append(out, l.line(i, grid, bodyWidth(width, bar), reach))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return alongside(out, bar, width)
}

// previewPane is the detail half of a wide queue: the row under the cursor,
// titled and windowed to the pane's interior.
func (l *List) previewPane(width, height int) []string {
	row := l.current()
	if row == nil {
		return preview.Pad(nil, height, width)
	}

	title := row.About.Subject
	if row.About.Prefix != "" {
		title = row.About.Prefix + "  " + title
	}
	if row.About.Ref != "" {
		title += "  " + row.About.Ref
	}

	var lines []string
	for _, f := range row.About.Facts {
		lines = append(lines, l.styles.role(f.Role, row.Dim, false).Render(f.Text))
	}
	for _, d := range row.Detail {
		lines = append(lines, l.styles.body.Render(d))
	}

	d := preview.Detail{Title: title, Lines: lines, Focused: true}
	st := preview.SkinStyles(l.styles.skin)

	return preview.Pad(append(
		[]string{d.Head(st, width, max(height-1, 0))}, d.Window(height-1)...),
		height, width)
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
		return strconv.Itoa(len(s.Rows)) + " · za shows"
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
			{enterKey, readTick},
			{spaceKey, readTick},
			{"i", replyWord},
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

	return l.use.brighten(l.keyContext(), hints)
}

// overBody draws what floats over the rows between the header and the footer:
// the palette, or the first-run card. The header and footer stay in view, so
// the reader keeps the map of where they are.
func (l *List) overBody(lines []string, f frame.Frame) []string {
	top := frame.Top(f)
	if top >= len(lines)-1 {
		return lines
	}

	body := lines[top : len(lines)-1]

	switch {
	case l.pal.IsOpen():
		body = l.pal.Over(body, l.width)
	case l.welcome.Showing():
		body = l.welcome.Over(l.styles.skin, body, l.width, len(body))
	}

	out := slices.Clone(lines[:top])
	out = append(out, body...)

	return append(out, lines[len(lines)-1])
}

// paletteRows is how many rows the palette is drawn over, the body and the
// inspector under it, which a click is read against.
func (l *List) paletteRows() int { return max(0, l.height-frame.Top(l.frame())-1) }

func defaultListHelp() [][2]string {
	return [][2]string{
		{"j/k, ctrl+u/d, gg/G", "move, half page, top and bottom"},
		{"ctrl+e/ctrl+y", "scroll without moving the cursor"},
		{tabKey, "the next group"},
		{"1/2/3, ]q/[q", "the queue to read"},
		{"]u/[u", "the next or previous row needing a look"},
		{"n/N", "the last motion again, or back"},
		{"/", "narrow to the rows carrying a word; esc puts them back"},
		{enterKey, "read the whole conversation, and mark it read"},
		{spaceKey, "mark read without opening it"},
		{"i", "reply, staged into the prepared review"},
		{"R", "resolve the thread, or thumbs-up what cannot be resolved"},
		{"o", "open it on GitHub"},
		{refreshKey, "read the queue again"},
		{"q, esc", "leave"},
		{},
		{"", "● marks a conversation that moved since you last read it."},
	}
}
