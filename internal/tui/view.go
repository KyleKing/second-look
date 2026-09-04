package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/seen"
)

// tabStop is how wide a tab renders. A diff of Go or Python is unreadable if
// tabs reach the terminal, which advances the cursor without writing cells.
const tabStop = 4

// View renders one frame into the alternate screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true

	return v
}

func (m *Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("second-look needs %dx%d, this is %dx%d", minWidth, minHeight, m.width, m.height)
	}

	body := m.rowLines()
	if m.help {
		body = m.helpLines()
	}

	return strings.Join(append(append([]string{m.title()}, body...), m.footerLines()...), "\n")
}

func (m *Model) title() string {
	c := m.counts()
	left := fmt.Sprintf("%s/%s #%d", m.review.Owner, m.review.Repo, m.review.Number)
	right := cut(fmt.Sprintf("%s · %s · %s%s%d ready · %d draft · %d skipped%s",
		m.position(), m.treeWord(), m.costCount(), m.readCount(),
		c.ready, c.draft, c.skip, todoCount(c)), m.width)

	if word := m.view.String(); word != "" {
		left += "  " + word
	}

	if m.drawn != renderPlain {
		left += "  " + m.drawn.String()
	}

	left += m.orderWord()

	// A head that moved outlives the footer message that announced it, because
	// every row under this line belongs to the diff it moved away from.
	if m.newHead != "" {
		left += "  head moved to " + short(m.newHead)
	}

	if path := fitPath(m.rowPath(), m.width-textWidth(right)-textWidth(left)-indent); path != "" {
		left += "  " + path
	}

	// The counts and the position are fixed width, so the path yields to them
	// rather than pushing the line past the frame.
	left = cut(left, max(1, m.width-textWidth(right)-1))
	gap := max(1, m.width-textWidth(left)-textWidth(right))

	return m.styles.title.Render(left) +
		strings.Repeat(" ", gap) + m.styles.subtitle.Render(right)
}

// fitPath keeps the part of a path that says which file it is. Cutting a path
// at either end loses that, so the base name stands in where the whole will not
// fit, and where even the base name will not, the title says nothing rather
// than something unreadable.
func fitPath(path string, room int) string {
	if path == "" || room < 1 {
		return ""
	}

	if textWidth(path) <= room {
		return path
	}

	base := path[strings.LastIndex(path, "/")+1:]
	if textWidth(base) <= room {
		return base
	}

	return ""
}

// treeWord is where the working copy stands, which C, !, and M each behave
// differently for. The screen knew it and said nothing, so the way to find out
// was to press a key and read the refusal.
func (m *Model) treeWord() string {
	switch m.tree {
	case TreeOnHead:
		return "on head"
	case TreeElsewhere:
		return "off head"
	case TreeNone:
		return "no clone"
	}

	return ""
}

// costCount is what the change is rated, absent until the structural pass
// answers and where nothing could parse, because a number standing for a hunk
// count alone would be read as one that means more than that.
func (m *Model) costCount() string {
	if !m.cost.Rated() {
		return ""
	}

	return fmt.Sprintf("cost %d · ", m.cost.Total)
}

// readCount is how much of the diff has been read, which is the number that
// says whether the review is finished. It is absent when nothing records it.
func (m *Model) readCount() string {
	if m.read == nil || m.view == viewComments {
		return ""
	}

	// Folding hides hunks nobody is being asked to read, so they leave the
	// count as well as the frame; otherwise it could never reach the total.
	refs := m.shownHunks()
	if len(refs) == 0 {
		return ""
	}

	return fmt.Sprintf("%d/%d read · ", m.read.Count(refs), len(refs))
}

// position is where the cursor is. On a comment it counts them, since "which
// of these am I on" is the question a review asks and the scrollbar answers in
// lines rather than in comments. Everywhere else it is how far down the screen
// is, fixed width so the title does not shift under the eye on every keystroke.
func (m *Model) position() string {
	if at, total := m.commentAt(); at > 0 {
		return fmt.Sprintf("comment %d/%d", at, total)
	}

	last := len(m.screen.rows) - 1
	if last < 1 {
		return "100%"
	}

	return fmt.Sprintf("%3d%%", m.cursor*100/last)
}

// commentAt is which comment the cursor is inside and how many the screen is
// showing, counted in the order they are drawn rather than the order they sit
// in the file: the number has to agree with what ]c walks. A folded-away
// comment leaves the count as well as the frame. Zero means the cursor is on
// something that is not a comment.
func (m *Model) commentAt() (int, int) {
	want := m.current()

	var at, total int

	for i := range m.screen.rows {
		r := m.screen.rows[i]
		if !isComment(r) {
			continue
		}

		total++

		if r.comment == want {
			at = total
		}
	}

	return at, total
}

// tally is how many comments will post, how many block the post, and how many
// were considered and declined.
type tally struct {
	ready int
	draft int
	skip  int
	todo  int
}

func (m *Model) counts() tally {
	var out tally

	for i := range m.review.Comments {
		switch m.review.Comments[i].Status {
		case artifact.StatusReady:
			out.ready++
		case artifact.StatusDraft:
			out.draft++
		case artifact.StatusSkip:
			out.skip++
		case artifact.StatusTodo:
			out.todo++
		}
	}

	return out
}

// rowPath is the file the cursor is in, and only once that file's own row has
// scrolled off the top: a heading the reader can already see is not worth the
// width, and the one that has gone is exactly what the eye has lost. The list
// screens carry their section heading the same way.
func (m *Model) rowPath() string {
	for i := min(m.cursor, len(m.screen.rows)-1); i >= 0; i-- {
		if m.screen.rows[i].kind != rowFile {
			continue
		}

		if i >= m.offset {
			return ""
		}

		return m.screen.rows[i].path
	}

	return ""
}

// maxFailLines caps how much of the frame a failure takes. A refusal from gh
// runs longer than one line, and the reason a post failed is the one message
// that must not be cut off mid-word.
const maxFailLines = 3

// footerLines is the bottom of the frame. Everything but a failure fits one
// line; a failure wraps, and whatever still does not fit reaches the
// scrollback when the screen exits.
func (m *Model) footerLines() []string {
	if m.searching {
		return []string{m.prompt()}
	}

	if m.status == "" {
		return []string{cut(" "+hintLine(m.styles, m.hints()), m.width)}
	}

	if !m.failed {
		return []string{m.styles.footer.Render(cut(m.status, m.width))}
	}

	lines := wrap(m.status, m.width)
	if len(lines) > maxFailLines {
		lines = append(lines[:maxFailLines-1:maxFailLines-1], lines[maxFailLines-1]+" …")
	}

	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, m.styles.fail.Render(cut(l, m.width)))
	}

	return out
}

// prompt is the search line. It says what the scope is rather than leaving it
// to be remembered, and names the key that changes it, because a search that
// silently skipped most of the diff would be the worst kind of wrong.
func (m *Model) prompt() string {
	scope := "all"
	if m.search.unread {
		scope = "unread"
	}

	head := m.styles.key.Render("/") + m.styles.footer.Render(scope+" ")
	tail := m.styles.footer.Render("  tab: scope")

	return cut(head+m.search.input.View()+tail, m.width)
}

// hints is what is actionable where the cursor is standing. The keys that
// change a comment are shown on a comment and the keys that skip through the
// diff are shown on code, because both sets at once do not fit an 80-column
// frame and quitting has to be visible in either.
func (m *Model) hints() [][2]string {
	var middle [][2]string

	switch {
	case m.currentThread() >= 0:
		middle = [][2]string{{"e", "reply"}}
	case m.current() >= 0:
		middle = [][2]string{{"m", "mark"}, {"e", "edit"}, {"z", "fold"}}
	case m.current() != noComment:
		middle = [][2]string{{"e", "write"}}
	case m.onCode():
		middle = [][2]string{{"a", "add"}}
	}

	view := [2]string{"c", m.view.next().String()}
	if m.view.next() == viewDiff {
		view = [2]string{"c", "the diff"}
	}

	hints := make([][2]string, 0, len(middle)+6)
	hints = append(hints, [2]string{"j/k", "line"}, [2]string{"]", "go to"}, view)
	hints = append(hints, middle...)

	return append(hints, [2]string{"S", "submit"}, [2]string{"?", "help"}, [2]string{"q", quitWord})
}

// onCode reports whether the cursor is on a line of the diff, which is the one
// place a new comment can anchor.
func (m *Model) onCode() bool {
	return m.cursor >= 0 && m.cursor < len(m.screen.rows) && m.screen.rows[m.cursor].kind == rowCode
}

func (m *Model) helpLines() []string {
	out := helpBlock(m.styles, helpLines(), m.width)
	if m.helpAt < len(out) {
		out = out[m.helpAt:]
	}

	for len(out) < m.viewHeight() {
		out = append(out, "")
	}

	return out[:m.viewHeight()]
}

// rowLines is the frame's body, with the editor standing in for the block it
// is writing so what is being answered stays where it was on the screen.
func (m *Model) rowLines() []string {
	h := m.viewHeight()
	bar := scrollbar(h, len(m.screen.rows), m.offset)
	width := bodyWidth(m.width, bar)

	out := make([]string, 0, h)

	if head, ok := m.pinned(width); ok {
		out = append(out, head)
	}

	for i := m.offset; i < len(m.screen.rows) && len(out) < h; i++ {
		if m.editingHere(i) {
			out = append(out, m.editorLines()...)
			i = m.spanEnd(i)

			continue
		}

		out = append(out, m.renderRow(i, width))

		if m.editingUnder(i) {
			out = append(out, m.editorLines()...)
		}
	}

	for len(out) < h {
		out = append(out, "")
	}

	return alongside(out[:h], bar, m.styles, m.width)
}

// cursorBar is the column every row spends on saying whether the cursor is on
// it. It is a glyph rather than a reversed row, so a wide terminal does not
// answer a keystroke with a bar of inverted text across it.
const cursorBar = "▌"

// rangeBar is the same column for a row the open range covers. It is thinner
// rather than paler, since the palette a terminal is on may quantize the two
// colors into one and leave the range invisible.
const rangeBar = "▏"

func (m *Model) renderRow(i, width int) string {
	body := m.rowBody(m.screen.rows[i], width)

	if i == m.cursor {
		return m.styles.cursor.Render(cursorBar) + body
	}

	if m.inRange(i) {
		return m.styles.selected.Render(rangeBar) + body
	}

	return " " + body
}

// rowBody is the row without the column that says where the cursor is. The
// rich renderer colors a line in pieces, so it hands back text already carrying
// its faces rather than one face for the whole row.
func (m *Model) rowBody(r row, width int) string {
	if r.kind == rowCode {
		switch {
		case m.sideBySide():
			return m.splitCode(r, width)
		case m.drawn != renderPlain:
			return m.richCode(r, width)
		}
	}

	text, style := m.rowContent(r)
	if r.kind == rowCode && m.behind(r) {
		style = m.styles.behind
	}

	if r.kind == rowFile {
		return m.ruledFile(text, width)
	}

	if r.lit != nil {
		return cut(m.styles.note.Render(m.threadLead())+m.pastedCode(r), width)
	}

	return style.Render(cut(text, width))
}

// threadLead is the rail a conversation hangs off, which every row of one
// spends whether it is drawn as prose or as code.
func (m *Model) threadLead() string {
	return strings.Repeat(" ", m.screen.numWidth+indent) + "│ "
}

// pastedCode draws a line of a fenced block inside a comment under the grammar
// the fence named. It takes the rich renderer's faces and none of its bands: a
// fence is code somebody quoted rather than code that changed.
//
// The spans index the row rather than the line the fence held, having been
// shifted onto it when it was built.
func (m *Model) pastedCode(r row) string {
	var b strings.Builder

	for _, piece := range spanRuns(r.lit, len(r.text)) {
		face, ok := m.rich.class[piece.class]
		if !ok {
			face = m.styles.note
		}

		b.WriteString(face.Render(r.text[piece.from:piece.to]))
	}

	return b.String()
}

// spanRuns cuts a line at every boundary the grammar drew, so a byte no span
// covers is still written once and written plainly.
func spanRuns(spans []highlight.Span, length int) []run {
	out := make([]run, 0, len(spans)*2+1)
	at := 0

	for _, s := range spans {
		from, to := min(s.From, length), min(s.To, length)
		if from > at {
			out = append(out, run{from: at, to: from, class: highlight.Plain})
		}

		if to > from {
			out = append(out, run{from: from, to: to, class: s.Class})
		}

		at = max(at, to)
	}

	if at < length {
		out = append(out, run{from: at, to: length, class: highlight.Plain})
	}

	return out
}

// ruleFloor is the shortest run of rule worth drawing, under which a heading
// ends at its name.
const ruleFloor = 4

// ruledFile is a file heading with a rule out to the frame's edge.
func (m *Model) ruledFile(text string, width int) string {
	head := m.styles.file.Render(cut(text, width))

	rest := width - textWidth(text) - 1
	if rest < ruleFloor {
		return head
	}

	return head + m.styles.hunk.Render(" "+strings.Repeat("\u2500", rest))
}

func (m *Model) rowContent(r row) (string, lipgloss.Style) {
	switch r.kind {
	case rowBlank:
		return "", m.styles.footer
	case rowGroup:
		return r.text, m.styles.title
	case rowFile:
		return "  " + r.text, m.styles.file
	case rowHunk:
		return "  " + m.readGlyph(r) + r.text, m.styles.hunk
	case rowComment, rowNote, rowTurn:
		return m.commentRow(r)
	case rowGone:
		// It sits in the column the sign of a kept line sits in, so what came
		// out reads as part of the file rather than as something said about it.
		return strings.Repeat(" ", m.screen.numWidth+1) + "▁ " + r.text, m.styles.remove
	case rowThread:
		return m.threadRow(r)
	case rowCode:
		return m.codeRow(r)
	}

	return r.text, m.styles.body
}

// commentRow draws a prepared comment. The bar is heavier than a thread's
// because this one is still being written, and the body is at the contrast of
// the code around it, since the prose is the thing being read on the rows it
// occupies. Only the note stays dim: it is evidence, not the finding.
func (m *Model) commentRow(r row) (string, lipgloss.Style) {
	text := strings.Repeat(" ", m.screen.numWidth+indent) + "┃ " + r.text

	switch {
	case r.kind == rowNote, r.kind == rowTurn:
		return text, m.styles.note
	case !r.head:
		return text, m.styles.body
	case r.comment < 0:
		// The review's own labels are the only headings inside the rail, so
		// they carry the weight that says a block starts here.
		return text, m.styles.rail.Bold(true)
	}

	return text, m.styles.forSeverity(m.review.Comments[r.comment].Severity)
}

// readGlyph marks a hunk already read. It is a glyph rather than a color so
// the one thing that says how much is left survives a monochrome terminal, and
// the unread case still spends the same two columns so nothing shifts.
func (m *Model) readGlyph(r row) string {
	if r.hunk == 0 || m.read == nil {
		return ""
	}

	if m.read.Has(seen.Hunk(m.diff, r.path, r.hunk)) {
		return "✓ "
	}

	return "  "
}

// threadRow renders a conversation already on GitHub. It shares the comment
// rail so it sits under its line the same way, and it is dimmer than a prepared
// comment, because nothing about it will change when this review posts.
func (m *Model) threadRow(r row) (string, lipgloss.Style) {
	text := m.threadLead() + r.text
	if r.head {
		return text, m.styles.rail
	}

	return text, m.styles.note
}

func (m *Model) codeRow(r row) (string, lipgloss.Style) {
	number, style := r.line.New, m.styles.add

	switch r.line.Kind {
	case diff.KindRemove:
		number, style = r.line.Old, m.styles.remove
	case diff.KindContext:
		style = m.styles.context
	}

	text := fmt.Sprintf("%*d %c %s", m.screen.numWidth, number, r.line.Kind, expand(r.line.Text))

	return text, style
}

// expand replaces tabs so a captured frame says what the terminal showed.
func expand(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}

	var (
		b    strings.Builder
		cols int
	)

	for _, r := range s {
		if r != '\t' {
			b.WriteRune(r)
			cols++

			continue
		}

		n := tabStop - cols%tabStop
		b.WriteString(strings.Repeat(" ", n))
		cols += n
	}

	return b.String()
}

// cut, pad, and textWidth all measure the cells a terminal spends, not runes
// and not bytes. A CJK glyph is two cells wide and a combining mark is none, so
// counting runes overruns the frame on one and stops short on the other.
func cut(s string, width int) string {
	if width < 1 {
		return ""
	}

	return ansi.Truncate(s, width, "…")
}

// cutTail keeps the end of what does not fit rather than the start, because the
// end is what names the thing: a pull request number, a line number, the file
// itself. Two threads on one long path render as the same row otherwise.
func cutTail(s string, width int) string {
	if width < 1 {
		return ""
	}

	over := textWidth(s) - width
	if over <= 0 {
		return s
	}

	return "…" + ansi.TruncateLeft(s, over+1, "")
}

func pad(s string, width int) string {
	if n := width - textWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}

	return s
}

func textWidth(s string) int { return ansi.StringWidth(s) }

// lpad right-aligns within a width, which is how a column of numbers is read.
func lpad(s string, width int) string {
	if n := width - textWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}

	return s
}

// todoCount is absent from the counts until there is one, since a review with
// no agent work waiting should not spend the width saying so.
func todoCount(c tally) string {
	if c.todo == 0 {
		return ""
	}

	return fmt.Sprintf(" · %d todo", c.todo)
}
