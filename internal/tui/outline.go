package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
)

// outline is the plan's chapters or features as a list, the selected one in
// detail beside it from tourWidth columns and under it below that.
type outline struct {
	open     bool
	features bool
	at       int
	feature  int
}

// listPane is one list of the outline and what it says about the selected row.
type listPane struct {
	head   string
	n, at  int
	row    func(i, w int) string
	detail func(w int) []string
}

const (
	listCols  = 96
	listShare = 45
	percent   = 100
	// A stacked list keeps at least minList rows and at most half the body.
	minList  = 4
	half     = 2
	modeCols = 6
	fileCols = 9
	lineCols = 12
)

// sizes is the lines each hunk adds and removes.
type sizes map[hunkAt][2]int

func hunkSizes(d *diff.Diff) sizes {
	out := sizes{}

	for i := range d.Files {
		p := filePath(&d.Files[i])

		for _, l := range d.Files[i].Lines {
			at := hunkAt{p, l.Hunk}
			n := out[at]

			switch l.Kind {
			case diff.KindAdd:
				n[0]++
			case diff.KindRemove:
				n[1]++
			default:
			}

			out[at] = n
		}
	}

	return out
}

func (s sizes) of(refs []order.Ref) (int, int) {
	added, removed := 0, 0

	for _, r := range refs {
		n := s[hunkAt{r.Path, r.Hunk}]
		added += n[0]
		removed += n[1]
	}

	return added, removed
}

func refPaths(refs []order.Ref) []string {
	var out []string

	for _, r := range refs {
		if !slices.Contains(out, r.Path) {
			out = append(out, r.Path)
		}
	}

	return out
}

// selectedChapter names the outline's selection, which a replan that reorders
// the chapters finds again by name.
func (m *Model) selectedChapter() string {
	if m.outline.at < len(m.plan.Chapters) {
		return m.plan.Chapters[m.outline.at].Name
	}

	return ""
}

func (m *Model) reselect(name string) {
	if at := slices.IndexFunc(m.plan.Chapters, func(c plan.Chapter) bool { return c.Name == name }); at >= 0 {
		m.outline.at = at
	}
}

func (m *Model) openOutline() {
	if len(m.plan.Chapters) == 0 {
		m.say("no chapters to outline", false)

		return
	}

	m.outline.open = true
	m.outline.at = m.chapterHere()
	m.say("", false)
}

// chapterHere is the chapter the cursor is reading, the first where it is in none.
func (m *Model) chapterHere() int {
	g, ok := m.tourGroup()

	switch {
	case !ok:
		return 0
	case g.chapter > 0:
		return g.chapter - 1
	}

	return max(0, m.chapterOf(g))
}

// readOutline walks the list and leaves it. Quit still quits, because a big
// change opens here and the outline is its first screen rather than an overlay.
func (m *Model) readOutline(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	const halfPage = 2

	n, at := len(m.plan.Chapters), &m.outline.at
	if m.outline.features {
		n, at = len(m.plan.Features), &m.outline.feature
	}

	switch {
	case key.Matches(msg, m.keys.Down):
		*at++
	case key.Matches(msg, m.keys.Up):
		*at--
	case key.Matches(msg, m.keys.HalfDown):
		*at += m.viewHeight() / halfPage
	case key.Matches(msg, m.keys.HalfUp):
		*at -= m.viewHeight() / halfPage
	case key.Matches(msg, m.keys.Top):
		*at = 0
	case key.Matches(msg, m.keys.Bottom):
		*at = n - 1
	case key.Matches(msg, m.keys.NextNote), key.Matches(msg, m.keys.PrevNote):
		m.switchList()
	case key.Matches(msg, m.keys.Accept):
		m.openSelected()

		return m, nil
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Outline):
		m.outline.open = false
		m.say("the diff · I the outline", false)

		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, func() tea.Msg { return leftMsg{} }
	case m.overlayKey(msg):
		return m, nil
	default:
		m.say("enter opens the chapter · esc the diff", false)
	}

	*at = clamp(*at, n-1)

	return m, nil
}

// switchList moves between the chapters and the features, where there are any.
func (m *Model) switchList() {
	if len(m.plan.Features) == 0 {
		m.say("no features: a chapter belongs to one when the agent names it", false)

		return
	}

	m.outline.features = !m.outline.features
	m.say("", false)
}

// openSelected reads the selected chapter, or the first chapter of the
// selected feature.
func (m *Model) openSelected() {
	at := clamp(m.outline.at, len(m.plan.Chapters)-1)

	if m.outline.features {
		at = m.plan.Features[clamp(m.outline.feature, len(m.plan.Features)-1)].Chapters[0]
		m.outline.at = at
	}

	m.openChapter(at)
}

// openChapter closes the outline on the chapter's heading, put at the top of
// the frame, and leaves a jump ctrl+o walks back.
func (m *Model) openChapter(n int) {
	from := m.snapshot()
	m.outline.open = false

	if m.view != viewDiff || m.asDiffed {
		m.view, m.whole, m.asDiffed = viewDiff, nil, false
		m.rebuild()
	}

	m.cursor = m.chapterRow(n)
	m.offset = clamp(m.cursor-1, m.maxOffset())
	m.jumpedFrom(from)
	m.say(fmt.Sprintf("chapter %d of %d · I the outline", n+1, len(m.plan.Chapters)), false)
}

// chapterRow is the row a chapter starts on: its heading, or where the tour drew
// a stop holding all of it, the first row of one of its hunks.
func (m *Model) chapterRow(n int) int {
	last := len(m.screen.rows) - 1

	for k, g := range m.screen.groups {
		if g.chapter == n+1 {
			return min(m.screen.heads[k]+1, last)
		}
	}

	refs := m.plan.Chapters[n].Hunks

	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.hunk > 0 && slices.Contains(refs, order.Ref{Path: r.path, Hunk: r.hunk}) {
			return i
		}
	}

	return 0
}

func (m *Model) outlineLines() []string {
	h := m.viewHeight()
	size := hunkSizes(m.diff)

	p := m.chapterPane(size)
	if m.outline.features && len(m.plan.Features) > 0 {
		p = m.featurePane(size)
	}

	if m.width >= tourWidth {
		lw := min(listCols, m.width*listShare/percent)
		dw := m.width - lw - indent - 1
		list := m.listLines(p, lw, h)
		detail := block(p.detail(dw), h, dw)
		bar := m.styles.edge.Render("│")

		out := make([]string, h)
		for i := range out {
			out[i] = pad(list[i], lw) + " " + bar + " " + detail[i]
		}

		return out
	}

	lh := min(p.n+1, max(minList, h/half))
	rest := h - lh - 1

	out := m.listLines(p, m.width, lh)
	out = append(out, m.styles.edge.Render(strings.Repeat("─", m.width-1)))

	return append(out, fitRows(p.detail(m.width), rest, m.width, m.styles.note.Render)...)
}

// fitRows is lines as exactly h rows, the last one saying how many did not fit.
func fitRows(lines []string, h, width int, face func(...string) string) []string {
	if len(lines) > h && h > 0 {
		more := len(lines) - h + 1
		lines = append(slices.Clone(lines[:h-1]), face("  ⋯ "+plural(more, "more line")))
	}

	return block(lines, h, width)
}

func (m *Model) chapterPane(size sizes) listPane {
	cs := m.plan.Chapters
	at := clamp(m.outline.at, len(cs)-1)
	read, folded, total := 0, 0, 0

	for _, c := range cs {
		added, removed := size.of(c.Hunks)
		total += added + removed

		if c.Mode == plan.ModeFold {
			folded += added + removed
		}

		if m.readAll(c.Hunks) {
			read++
		}
	}

	head := " " + m.styles.head.Render(countHead("chapters", at+1, len(cs)))
	facts := fmt.Sprintf(" · %d read", read)

	if folded > 0 {
		facts += fmt.Sprintf(" · %s of %s lines folded", humanize.Count(folded), humanize.Count(total))
	}

	return listPane{
		head: head + m.styles.note.Render(facts), n: len(cs), at: at,
		row:    func(i, w int) string { return m.outlineRow(size, cs[i], w) },
		detail: func(w int) []string { return m.chapterDetail(size, at, w) },
	}
}

// listLines is a pane's list in h rows, scrolled to keep the selection mid-frame.
func (m *Model) listLines(p listPane, w, h int) []string {
	out := []string{cut(p.head, w)}
	room := h - 1
	top := clamp(p.at-room/half, p.n-room)

	for i := top; i < p.n && len(out) < h; i++ {
		text := p.row(i, min(w, listCols)-1)

		if i != p.at {
			out = append(out, " "+text)

			continue
		}

		out = append(out, lightLine(m.styles.cursor.Render(cursorBar)+text, w, m.styles.selection.GetBackground()))
	}

	return block(out, h, w)
}

func (m *Model) outlineRow(size sizes, c plan.Chapter, w int) string {
	added, removed := size.of(c.Hunks)
	lines := "+" + humanize.Count(added) + " −" + humanize.Count(removed)

	if c.Mode == plan.ModeFold {
		lines = humanize.Count(added+removed) + " folded"
	}

	tail := pad(string(c.Mode), modeCols) + right(plural(len(refPaths(c.Hunks)), fileNoun), fileCols) +
		right(lines, lineCols)
	read := m.readAll(c.Hunks)
	name := cut(c.Source.Glyph()+" "+c.Name, w-indent-textWidth(tail)-1)
	gap := strings.Repeat(" ", max(1, w-indent-textWidth(name)-textWidth(tail)))
	face := m.styles.body.Render

	if read || c.Mode == plan.ModeFold {
		face = m.styles.behind.Render
	}

	return m.railMark(false, read) + face(name) + gap + m.styles.note.Render(tail)
}

func right(s string, w int) string { return strings.Repeat(" ", max(0, w-textWidth(s))) + s }

// chapterDetail is what the outline says about one chapter: why it is read
// together, which files it holds and where else each one is read, and its stops.
func (m *Model) chapterDetail(size sizes, at, w int) []string {
	c := m.plan.Chapters[at]
	room := w - indent

	var out []string

	add := func(text string, face func(...string) string) {
		for _, l := range wrap(text, room) {
			out = append(out, " "+face(l))
		}
	}

	add(fmt.Sprintf("CHAPTER %d OF %d · %s", at+1, len(m.plan.Chapters), c.Mode), m.styles.head.Render)
	add(c.Source.Glyph()+" "+c.Name, m.styles.file.Render)

	if c.Why != "" {
		add(c.Why, m.styles.body.Render)
	}

	placed := "placed by " + sourceWord(c.Source)
	if c.Feature != "" {
		placed += " · part of " + c.Feature
	}

	add(placed, m.styles.note.Render)

	if p := c.Pattern; p != nil {
		out = append(out, "", " "+m.styles.head.Render("SHAPE · "+plural(len(p.Members), fileNoun)))

		for _, line := range m.shapeWords(p) {
			add(line, m.styles.note.Render)
		}
	}

	if asks := m.asksIn(c); len(asks) > 0 {
		out = append(out, "", " "+m.styles.head.Render(fmt.Sprintf("ASKED · %d", len(asks))))

		for _, n := range asks {
			a := m.plan.Asks[n]
			add(place(a.Path, a.Side, firstOf(a.StartLine, a.Line), a.Line)+" · "+a.Question, m.styles.body.Render)
		}
	}

	files := refPaths(c.Hunks)
	out = append(out, "", " "+m.styles.head.Render(fmt.Sprintf("FILES · %d", len(files))))

	col := 0
	for _, p := range files {
		col = max(col, textWidth(p))
	}

	col = min(col, room/half)

	for _, p := range files {
		out = append(out, m.fileLine(size, c, p, at, col))
	}

	if stops := m.stopsIn(c); len(stops) > 0 {
		out = append(out, "", " "+m.styles.head.Render(fmt.Sprintf("STOPS · %d", len(stops))))

		for _, n := range stops {
			s := m.plan.Stops[n]
			add(fmt.Sprintf("%d %s", n+1, strings.TrimPrefix(s.Role+" · "+s.What, " · ")), m.styles.body.Render)
		}
	}

	return out
}

// fileLine is one file of a chapter, its path in col cells: how much of it the
// chapter holds, and the other chapters reading the rest of it.
func (m *Model) fileLine(size sizes, c plan.Chapter, path string, at, col int) string {
	var mine []order.Ref

	for _, r := range c.Hunks {
		if r.Path == path {
			mine = append(mine, r)
		}
	}

	added, removed := size.of(mine)
	facts := fmt.Sprintf("  %s  +%d −%d", plural(len(mine), "hunk"), added, removed)

	if also := m.alsoIn(path, at); also != "" {
		facts += "  also " + also
	}

	name := pad(cutTail(path, col), col)

	return " " + m.railMark(false, m.readAll(mine)) + m.styles.body.Render(name) + m.styles.note.Render(facts)
}

// alsoIn names the other chapters that read some of a file, by number.
func (m *Model) alsoIn(path string, at int) string {
	var out []string

	for i, c := range m.plan.Chapters {
		if i != at && slices.ContainsFunc(c.Hunks, func(r order.Ref) bool { return r.Path == path }) {
			out = append(out, strconv.Itoa(i+1))
		}
	}

	if len(out) == 0 {
		return ""
	}

	return "in " + strings.Join(out, ", ")
}

// stopsIn is the stops whose hunk the chapter holds, by index.
func (m *Model) stopsIn(c plan.Chapter) []int {
	var out []int

	for i, s := range m.plan.Stops {
		if slices.Contains(c.Hunks, s.Ref) {
			out = append(out, i)
		}
	}

	return out
}

func (m *Model) outlineHints() []hint {
	what, other := chapterNoun, "features"
	if m.outline.features {
		what, other = featureNoun, "chapters"
	}

	return []hint{
		{key: walkKeys, what: what},
		{key: enterKey, what: "open"},
		{key: tabKey, what: other, off: len(m.plan.Features) == 0},
		{key: escKey, what: "the diff"},
		{key: "?", what: m.keys.Help.Help().Desc},
		{key: "q", what: quitWord},
	}
}

func (m *Model) outlineWord() string {
	switch {
	case m.outline.open && m.outline.features:
		return "features"
	case m.outline.open:
		return "outline"
	}

	return ""
}
