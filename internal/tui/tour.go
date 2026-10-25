package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
)

// A change past tourFiles opens on the plain screen. The tour draws its rail
// and margin beside the diff from tourWidth columns, and inline below that.
const (
	tourFiles  = 60
	tourWidth  = 160
	railCols   = 30
	marginCols = 44
	labelCols  = 16
	// The rail spends railGap on its mark and the spaces around a chapter's mode.
	railGap = 4
	// A chapter's name gives glyphGap to its source glyph and the space either side of it.
	glyphGap = 3
	// The rail spends railEdge on its rule and the column before it.
	railEdge = 2
	// The margin spends marginRule on the space and rule that part it from the scrollbar.
	marginRule = 2
)

// touring reports whether the diff is walked as a tour. A plan of one stop or
// chapter has nothing to walk, so it draws as the plain screen.
func (m *Model) touring() bool {
	return m.tour && m.view == viewDiff && !m.asDiffed && len(m.plan.Stops)+len(m.plan.Chapters) > 1
}

func (m *Model) tourWide() bool { return m.touring() && m.width >= tourWidth }

// diffWidth is the columns the diff itself is drawn in, which the tour's rail
// and margin take from at full width.
func (m *Model) diffWidth() int {
	if m.tourWide() {
		return m.width - railCols - marginCols
	}

	return m.width
}

func (m *Model) toggleTour() {
	was := m.here()

	m.tour = !m.tour
	if m.tour && m.view != viewDiff {
		m.view = viewDiff
	}

	m.rebuild()
	m.goTo(was)

	switch {
	case !m.tour:
		m.say("the chapters without the tour", false)
	case !m.touring():
		m.say("a plan of one chapter has nothing to tour", false)
	case len(m.plan.Stops) == 0:
		m.say("the tour, chapter by chapter: nothing has named its stops", false)
	default:
		m.say("the tour", false)
	}
}

// tourMotion is what n walks before any motion is named: the stops, or the
// chapters where there are none.
func (m *Model) tourMotion() motion {
	if len(m.plan.Stops) > 0 {
		return stopMotion(1)
	}

	return chapterMotion(1)
}

func stopMotion(step int) motion { return motion{step: step, what: stopNoun, want: isKind(rowStop)} }

func chapterMotion(step int) motion {
	return motion{step: step, what: chapterNoun, want: isKind(rowGroup)}
}

// stopGroups is one group per stop, ahead of the chapters, holding the stop's
// hunk; a hunk two stops name is drawn under the first.
func (l layout) stopGroups(at map[string]int, drawn []bool) ([]planGroup, map[order.Ref]bool) {
	taken := map[order.Ref]bool{}

	if !l.tour {
		return nil, taken
	}

	out := make([]planGroup, 0, len(l.stops))

	for n, s := range l.stops {
		i, ok := at[s.Ref.Path]
		if !ok || taken[s.Ref] {
			continue
		}

		taken[s.Ref], drawn[i] = true, true

		name := fmt.Sprintf("stop %d of %d", n+1, len(l.stops))
		if s.Role != "" {
			name += " · " + s.Role
		}

		out = append(out, planGroup{
			name: name, mode: plan.ModeRead, source: s.Source, stop: n + 1, files: 1, hunks: 1,
			parts: []part{{file: i, path: s.Ref.Path, hunks: map[int]bool{s.Ref.Hunk: true}}},
		})
	}

	return out, taken
}

func (l layout) stopNotes(g planGroup) []row {
	if g.stop == 0 {
		return nil
	}

	const lead = "   "

	s := l.stops[g.stop-1]
	room := max(1, l.width-len(lead)-indent*2)

	var out []row

	for _, line := range wrap(s.What, room) {
		out = append(out, row{kind: rowWhat, text: lead + line, comment: -1})
	}

	for i, line := range wrap(s.Then, room) {
		arrow := "→ "
		if i > 0 {
			arrow = "  "
		}

		out = append(out, row{kind: rowThen, text: lead + arrow + line, comment: -1})
	}

	return out
}

// tourAt is which of the drawn groups the cursor is in, or -1 where none was drawn.
func (m *Model) tourAt() int {
	if len(m.screen.heads) == 0 {
		return -1
	}

	at := 0

	for k, h := range m.screen.heads {
		if h > m.cursor {
			break
		}

		at = k
	}

	return at
}

func (m *Model) tourGroup() (planGroup, bool) {
	at := m.tourAt()
	if at < 0 {
		return planGroup{}, false
	}

	return m.screen.groups[at], true
}

// bodyLines is the rows, with the tour's rail and margin beside them where the
// frame is wide enough to hold all three.
func (m *Model) bodyLines() []string {
	rows := m.rowLines()
	if !m.tourWide() {
		return rows
	}

	mid := m.diffWidth()
	rail, margin := m.railLines(len(rows)), m.marginLines(len(rows))

	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rail[i] + pad(rows[i], mid) + margin[i]
	}

	return out
}

func (m *Model) railLines(h int) []string {
	g, _ := m.tourGroup()
	w := railCols - railEdge

	var out []string

	line := func(text string, style func(...string) string) {
		out = append(out, pad(style(cut(" "+text, w)), w+1))
	}

	if n := len(m.plan.Stops); n > 0 {
		line(countHead("stops", g.stop, n), m.styles.head.Render)

		for i, s := range m.plan.Stops {
			line(m.railMark(i+1 == g.stop, m.readAll(stopRefs(s)))+strconv.Itoa(i+1)+" "+s.What,
				m.railFace(i+1 == g.stop))
		}

		line("", m.styles.note.Render)
	}

	holding := m.chapterOf(g)
	names := make([]string, len(m.plan.Chapters))

	for i, c := range m.plan.Chapters {
		names[i] = c.Name
	}

	line(countHead("chapters", g.chapter, len(m.plan.Chapters)), m.styles.head.Render)

	for i, c := range m.plan.Chapters {
		here := i+1 == g.chapter || (g.stop > 0 && i == holding)
		name := c.Source.Glyph() + " " + fitName(c.Name, names, w-len(c.Mode)-railGap-glyphGap)
		gap := strings.Repeat(" ", max(1, w-railGap-textWidth(name)-len(c.Mode)))

		line(m.railMark(here, m.readAll(c.Hunks))+name+gap+string(c.Mode), m.railFace(here))
	}

	line("", m.styles.note.Render)

	for _, l := range wrap(m.plan.Basis, w-1) {
		line(l, m.styles.note.Render)
	}

	if n := len(m.plan.Problems); n > 0 {
		line("⚠ "+plural(n, "plan problem")+" · i", m.styles.warn.Render)
	}

	line("[p] plain  [I] outline", m.styles.note.Render)

	return m.divided(block(out, h, w+1), true)
}

// divided draws the rule between a tour panel and the diff, on the side facing it.
func (m *Model) divided(lines []string, left bool) []string {
	rule := m.styles.edge.Render(panelRule)

	for i, l := range lines {
		if left {
			lines[i] = l + rule
		} else {
			lines[i] = " " + rule + l
		}
	}

	return lines
}

const panelRule = "│"

func countHead(what string, at, n int) string {
	if at == 0 {
		return fmt.Sprintf("%s · %d", strings.ToUpper(what), n)
	}

	return fmt.Sprintf("%s · %d of %d", strings.ToUpper(what), at, n)
}

func (*Model) railMark(here, read bool) string {
	switch {
	case here:
		return "▸ "
	case read:
		return "✓ "
	}

	return "  "
}

func (m *Model) railFace(here bool) func(...string) string {
	if here {
		return m.styles.selected.Render
	}

	return m.styles.body.Render
}

func (m *Model) marginLines(h int) []string {
	g, ok := m.tourGroup()
	w := marginCols - indent - marginRule

	var out []string

	add := func(text string, style func(...string) string) {
		for _, l := range wrap(text, w) {
			out = append(out, pad(" "+style(l), marginCols-marginRule))
		}
	}

	switch {
	case !ok:
	case g.stop > 0:
		s := m.plan.Stops[g.stop-1]
		add(strings.ToUpper(g.name), m.styles.head.Render)
		add(s.What, m.styles.body.Render)

		if s.Then != "" {
			out = append(out, "")
			add("→ "+s.Then, m.styles.note.Render)
		}

		out = append(out, "")
		add(s.Source.Glyph()+" "+sourceWord(s.Source), m.styles.note.Render)

		if at := m.chapterOf(g); at >= 0 {
			out = append(out, "")
			m.chapterNote(at, add)
		}
	case g.chapter > 0:
		m.chapterNote(g.chapter-1, add)
	}

	if next := m.nextGroup(); next != "" {
		out = append(out, "")
		add("next: "+next, m.styles.note.Render)
	}

	return m.divided(block(out, h, marginCols-marginRule), false)
}

func (m *Model) chapterNote(at int, add func(string, func(...string) string)) {
	c := m.plan.Chapters[at]
	add(fmt.Sprintf("CHAPTER %d OF %d · %s", at+1, len(m.plan.Chapters), c.Mode), m.styles.head.Render)
	add(c.Name, m.styles.file.Render)

	if c.Why != "" {
		add(c.Why, m.styles.body.Render)
	}

	add(c.Source.Glyph()+" "+sourceWord(c.Source), m.styles.note.Render)

	if c.Feature != "" {
		add("part of "+c.Feature, m.styles.note.Render)
	}
}

func (m *Model) nextGroup() string {
	at := m.tourAt()
	if at < 0 || at+1 >= len(m.screen.groups) {
		return ""
	}

	g := m.screen.groups[at+1]
	if g.stop > 0 {
		return g.name + " · " + m.plan.Stops[g.stop-1].What
	}

	return g.name
}

// chapterOf is which chapter holds a stop's hunk, or -1.
func (m *Model) chapterOf(g planGroup) int {
	if g.stop == 0 {
		return -1
	}

	ref := m.plan.Stops[g.stop-1].Ref

	return slices.IndexFunc(m.plan.Chapters, func(c plan.Chapter) bool { return slices.Contains(c.Hunks, ref) })
}

func sourceWord(s plan.Source) string {
	switch s {
	case plan.SourceRule:
		return "rule"
	case plan.SourceGraph:
		return "code"
	case plan.SourceAgent:
		return "agent"
	case plan.SourceYou:
		return "you"
	case plan.SourceCommit:
		return "commit"
	}

	return string(s)
}

func stopRefs(s plan.Stop) []order.Ref { return []order.Ref{s.Ref} }

func (m *Model) readAll(refs []order.Ref) bool {
	for _, r := range refs {
		if !m.done[hunkAt{r.Path, r.Hunk}] {
			return false
		}
	}

	return len(refs) > 0
}

// block is lines as exactly h rows, each padded to width.
func block(lines []string, h, width int) []string {
	out := make([]string, h)
	blank := strings.Repeat(" ", width)

	for i := range out {
		out[i] = blank
		if i < len(lines) {
			out[i] = pad(lines[i], width)
		}
	}

	return out
}

// tourStrip is the rail as one line: a segment per stop and chapter sized by
// what it changes, named while there is room to.
func (m *Model) tourStrip(room int) string {
	at := m.tourAt()
	if at < 0 {
		return ""
	}

	cs := make([]cluster, len(m.screen.groups))
	names := m.groupNames()
	through := m.groupThrough(at)

	for k, g := range m.screen.groups {
		label := strconv.Itoa(g.stop)
		if g.stop == 0 {
			label = fitName(g.name, names, labelCols)
		}

		cs[k] = cluster{label: label, segs: []seg{{
			weight: m.changed(g), read: m.readAll(groupRefs(g, m.diff)), lit: k == at, marked: k == at, at: through,
		}}}
	}

	for _, labels := range []bool{true, false} {
		if widths, ok := stripWidths(cs, labels, room); ok {
			return m.draw(cs, widths, labels)
		}
	}

	widths := make([]int, len(cs))
	for i := range widths {
		widths[i] = 1
	}

	return cut(m.draw(cs, widths, false), room)
}

// groupThrough is how far the cursor stands through the drawn group at.
func (m *Model) groupThrough(at int) float64 {
	lo, hi := m.screen.heads[at], len(m.screen.rows)
	if at+1 < len(m.screen.heads) {
		hi = m.screen.heads[at+1]
	}

	return span(m.cursor, lo, hi-1)
}

func span(at, lo, hi int) float64 {
	if hi <= lo {
		return 0
	}

	return float64(min(max(at, lo), hi)-lo) / float64(hi-lo)
}

func (m *Model) groupNames() []string {
	out := make([]string, len(m.screen.groups))
	for k, g := range m.screen.groups {
		out[k] = g.name
	}

	return out
}

// fitName fits a chapter's name to room, keeping a path's end, since chapters
// named for directories share their first segments.
func fitName(name string, all []string, room int) string {
	if !strings.Contains(name, "/") {
		return cut(name, room)
	}

	return cutTail(shortestTail(name, all), room)
}

func groupRefs(g planGroup, d *diff.Diff) []order.Ref {
	var out []order.Ref

	for _, p := range g.parts {
		for _, l := range d.Files[p.file].Lines {
			ref := order.Ref{Path: p.path, Hunk: l.Hunk}
			if (p.hunks == nil || p.hunks[l.Hunk]) && !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}

	return out
}

func (m *Model) changed(g planGroup) int {
	n := 0

	for _, p := range g.parts {
		for _, l := range m.diff.Files[p.file].Lines {
			if (p.hunks == nil || p.hunks[l.Hunk]) && (l.Kind == diff.KindAdd || l.Kind == diff.KindRemove) {
				n++
			}
		}
	}

	return n
}
