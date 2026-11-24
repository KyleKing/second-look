package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/tui/frame"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/overlay"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/highlight"
)

// tabStop is how wide a tab renders. A diff of Go or Python is unreadable if
// tabs reach the terminal, which advances the cursor without writing cells.
const tabStop = 4

// signRoom is what a code line's prefix spends after its number: the space,
// the sign, and the space before the text.
const signRoom = 3

// View renders one frame into the alternate screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true

	// Capturing the mouse leaves drag-select to shift or option, which is the
	// price of a click moving the cursor.
	v.MouseMode = tea.MouseModeCellMotion

	return v
}

func (m *Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("second-look needs %dx%d, this is %dx%d",
			minWidth, minHeight, m.width+pageSides*m.padX, m.height+m.padY)
	}

	var body []string
	switch {
	case m.building:
		body = m.loadingLines()
	case m.outline.open:
		body = m.outlineLines()
	default:
		body = m.bodyLines()
	}

	if m.waiting {
		body = m.waitLines(body)
	}

	switch {
	case m.pal.IsOpen():
		body = m.pal.Over(body, m.width)
	case m.aboutOpen:
		body = m.aboutLines()
	case m.showing != nil:
		body = m.answerLines()
	case m.labeler != nil:
		body = m.labeler.box(m.styles, body, m.width)
	}

	files := m.stats()
	out := append([]string{m.title(files), m.modes(files)}, body...)
	if m.pane != nil {
		out = append(out, m.paneLines()...)
	} else if m.agentOpen && m.agent != nil {
		out = append(out, m.agentLines()...)
	}

	return marginate(strings.Join(append(out, m.footerLines()...), "\n"), m.padX, m.padY)
}

// loadingLines is the page a frame draws while its rows are being built off
// the input goroutine, which a diff past asyncRows does on every switch.
func (m *Model) loadingLines() []string {
	n := m.viewHeight()
	out := make([]string, n)

	if n > 0 {
		out[n/3] = m.styles.note.Render("building the view…")
	}

	return out
}

// paneLines is the running program's screen under a divider naming it. The
// rows it returns are fixed-height: the emulator's screen is exactly the pane
// even where the program has drawn nothing yet.
func (m *Model) paneLines() []string {
	head := " " + m.paneTitle + "  keys go to it · ctrl+\\ ends it"

	rest := m.width - textWidth(head) - 1
	if rest >= ruleFloor {
		head += " " + strings.Repeat("─", rest)
	}

	return append([]string{m.styles.hunk.Render(cut(head, m.width))}, m.pane.Lines()...)
}

// name is what the title calls the review: the pull request by its number,
// and a local review by what it is diffing.
func (m *Model) name() string {
	if !m.review.Local {
		return fmt.Sprintf("%s#%d", m.review.Repo, m.review.Number)
	}

	name := m.review.Repo
	if m.review.Owner != "" {
		name = m.review.Owner + "/" + name
	}
	if m.review.HeadRef != "" {
		name += " · " + m.review.HeadRef
	}
	if m.review.BaseRef != "" {
		name += " vs " + m.review.BaseRef
	}

	return name
}

// shortestTail is the fewest trailing segments of a path that no other path in
// all ends with, so two index.ts files read as auth/index.ts and ui/index.ts. A
// path another one ends with has no shorter name than itself.
func shortestTail(path string, all []string) string {
	parts := strings.Split(path, "/")

	for n := 1; n < len(parts); n++ {
		tail := strings.Join(parts[len(parts)-n:], "/")

		if !slices.ContainsFunc(all, func(other string) bool {
			return other != path && (other == tail || strings.HasSuffix(other, "/"+tail))
		}) {
			return tail
		}
	}

	return path
}

// treeWord is where the working copy stands, which C, !, and M each behave
// differently for. The screen knew it and said nothing, so the way to find out
// was to press a key and read the refusal.
func (m *Model) treeWord() string {
	switch m.tree {
	case TreeOnHead:
		return ""
	case TreeElsewhere:
		return "off head"
	case TreeNone:
		return "no clone"
	}

	return ""
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

	return fmt.Sprintf("read %d of %d hunks", m.read.Count(refs), len(refs))
}

// position is where the cursor is. On a comment it counts them, since "which
// of these am I on" is the question a review asks and the scrollbar answers in
// lines rather than in comments. On code it is how far down the screen is,
// fixed width so the title does not shift under the eye on every keystroke of
// ordinary reading. Standing on a heading instead of a line of code is rarer
// and already moves the title elsewhere, so there the percentage carries the
// name of what the heading is, since a reader landing on one by a motion key
// is asking what it is as much as where it is.
func (m *Model) position() string {
	if at, total := m.commentAt(); at > 0 {
		return fmt.Sprintf("comment %d/%d", at, total)
	}

	last := len(m.screen.rows) - 1
	pct := percent

	if last >= 1 {
		pct = m.cursor * percent / last
	}

	if word := m.onWord(); word != "" {
		return fmt.Sprintf("%s %3d%%", word, pct)
	}

	return fmt.Sprintf("%3d%%", pct)
}

// onWord names a file's or a hunk's own heading, the two places a reader lands
// by a motion key (]f, ]h) rather than by scrolling, and so is asking what the
// row is as much as where it is. Everything else — code, a comment, a thread's
// card — already says what it is by how it is drawn, so naming it here too
// would only repeat the frame back at the reader.
func (m *Model) onWord() string {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return ""
	}

	switch m.screen.rows[m.cursor].kind {
	case rowFile:
		return fileNoun
	case rowHunk:
		return hunkNoun
	default:
		return ""
	}
}

// commentAt is which comment the cursor is inside and how many the screen is
// showing, counted in the order they are drawn rather than the order they sit
// in the file: the number has to agree with what ]c walks. A folded-away
// comment leaves the count as well as the frame. Zero means the cursor is on
// something that is not a comment.
func (m *Model) commentAt() (int, int) {
	want := m.current()
	if want < 0 {
		return 0, 0
	}

	var at, total int

	for i := range m.screen.rows {
		r := &m.screen.rows[i]
		if !isComment(*r) {
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

// cursorPath is the file the cursor stands in, taken from the nearest row above
// that belongs to one so a blank between two hunks does not read as no file.
func (m *Model) cursorPath() string {
	for i := min(m.cursor, len(m.screen.rows)-1); i >= 0; i-- {
		if p := m.screen.rows[i].path; p != "" {
			return p
		}
	}

	return ""
}

// maxFailLines caps how much of the frame a failure takes. A refusal from gh
// runs longer than one line, and the reason a post failed is the one message
// that must not be cut off mid-word.
const maxFailLines = 3

// statusGap is the cells a status and the hints beside it spend between them:
// one to lead the line and two apart.
const statusGap = 3

// footerHeight is how many lines footerLines draws, without drawing them
// where the answer is one. The screen's height is asked several times a
// keystroke, and fitting the key hints to measure a line that is always one
// line tall was a fifth of scrolling.
func (m *Model) footerHeight() int {
	switch {
	case m.searching, m.files.typing, m.drafting != nil, m.labeler != nil:
		return 1
	case m.status != "" && m.failed, m.band && m.status == "":
		return len(m.footerLines())
	}

	return 1
}

// footerLines is the bottom of the frame. Everything but a failure fits one
// line; a failure wraps, and whatever still does not fit reaches the
// scrollback when the screen exits.
func (m *Model) footerLines() []string {
	if m.searching {
		return []string{m.prompt()}
	}

	if m.files.typing {
		return []string{m.filesPrompt()}
	}

	if m.drafting != nil {
		return []string{m.questionPrompt()}
	}

	if m.labeler != nil {
		return []string{m.labeler.prompt(m.styles, m.width)}
	}

	if m.status == "" {
		return m.quietFooter()
	}

	// A status message shares the line with the hints rather than replacing
	// them: they are the map a reader orients by, and the message is where they
	// land right after asking for a change.
	if !m.failed {
		status := m.styles.footer.Render(cut(m.status, m.width))
		room := m.width - lipgloss.Width(status) - statusGap
		if line := keyhint.Fit(footerStyles(m.styles), m.hints(), room); line != "" {
			return []string{" " + status + "  " + line}
		}

		return []string{status}
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
	tail := m.styles.footer.Render("  [tab] scope")

	return cut(head+m.search.input.View()+tail, m.width)
}

// hints are the footer's keys. A key with nothing to act on under the cursor is
// dimmed rather than dropped, so the footer says what the screen offers as well
// as what it offers here, and a narrow frame sheds the dim ones first.
func (m *Model) hints() []keyhint.Hint {
	if m.outline.open {
		return m.outlineHints()
	}

	view := keyhint.Hint{Key: "v", What: m.view.next().String(), Priority: shedFirst}
	if m.view.next() == viewDiff {
		view.What = "the diff"
	}

	out := []keyhint.Hint{
		{Key: walkKeys, What: "line", Priority: shedFirst},
		{Key: "]", What: "go to", Priority: shedLater},
		view,
		{Key: "m", What: "mark", Priority: shedLater},
		{Key: "e", What: m.writeWord(), Priority: shedLater},
		{Key: "a", What: "add", Priority: shedLater},
		{Key: "i", What: "info", Priority: shedFirst},
		{Key: "S", What: "submit", Priority: shedLast},
		{Key: "?", What: "help", Priority: shedLast},
		{Key: escKey, What: "queue", Priority: shedFirst},
		{Key: "q", What: quitWord, Priority: shedLast},
	}

	if m.repoDir != "" {
		at := slices.IndexFunc(out, func(h keyhint.Hint) bool { return h.Key == "S" })
		out = slices.Insert(out, at, keyhint.Hint{Key: "B", What: "branches", Priority: shedFirst})
	}

	if m.agentStart != nil {
		at := slices.IndexFunc(out, func(h keyhint.Hint) bool { return h.Key == "S" })
		out = slices.Insert(out, at, keyhint.Hint{Key: "T", What: m.dispatchWord(), Priority: shedLater})
	}

	if m.view == viewFile {
		out = slices.DeleteFunc(out, func(h keyhint.Hint) bool { return h.Key == escKey })
		out = append([]keyhint.Hint{{Key: escKey, What: "back", Priority: shedLast}}, out...)
	}

	for i := range out {
		if a := m.gate(out[i].Key); !a.OK {
			out[i].Off, out[i].Why = true, a.Why
		}
	}

	return m.use.brighten(ctxReview, out)
}

// writeWord is what e does where the cursor is: answer a conversation, rewrite
// a staged comment, or write the review's own prose.
func (m *Model) writeWord() string {
	switch {
	case m.currentThread() >= 0:
		return replyWord
	case m.current() >= 0:
		return editWord
	}

	return "write"
}

// canWrite reports whether e has anything to write here, which is everything
// but a line of the diff: a new comment on one of those is a then a severity.
func (m *Model) canWrite() bool {
	return m.currentThread() >= 0 || m.current() != noComment
}

// onCode reports whether the cursor is on a line of the diff, which is the one
// place a new comment can anchor.
func (m *Model) onCode() bool {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return false
	}

	r := m.screen.rows[m.cursor]

	return r.kind == rowCode && (m.view != viewFile || r.hunk != 0)
}

// helpTree is the legend with the H chord's page filled in from the rounds
// this review was read at, which is the one page a static tree cannot write.
func (m *Model) helpTree() []keyhint.Hint {
	return m.use.brighten(ctxReview, helpTree(m.roundPage(time.Now())))
}

// helpWidth is the inner width of a legend's box: five eighths of a frame
// that is width cells wide, between a floor and a ceiling.
func helpWidth(width int) int {
	const (
		floor, ceiling = 44, 76
		numerator      = 5
		denominator    = 8
	)

	return min(max(floor, width*numerator/denominator), ceiling, width-boxMargin)
}

// roundBoxWidth is the most room the H chord's page takes, since a round's line
// carries who pushed it and how the todos stand.
const roundBoxWidth = 76

// pageRows draws a page of the legend. The H page is columns, so it is drawn
// unwrapped and only as wide as its rows.
func (m *Model) pageRows(path []string, page []keyhint.Hint) []string {
	if len(path) == 1 && path[0] == "H" {
		return m.roundRows(min(roundBoxWidth, m.width-boxMargin))
	}

	return keyhint.Column(legendStyles(m.styles), page, helpWidth(m.width))
}

// waitLines lays the page a waiting chord accepts over the diff's bottom
// corner, under a title naming the prefix. The page is the same one the
// legend shows for the key, so the two never disagree about what a chord is
// waiting on.
func (m *Model) waitLines(body []string) []string {
	page, ok := keyhint.At(m.helpTree(), []string{string(m.pending)})
	if !ok {
		return body
	}

	rows := m.pageRows([]string{string(m.pending)}, page)
	box := overlay.Box(boxStyles(m.styles), string(m.pending)+" then", rows, len(rows), 0)

	return overlay.Blit(body, box, m.width-textWidth(box[0])-1, len(body)-len(box))
}

// rowLines is the frame's body, with the editor standing in for the comment it
// is rewriting so the text stays where it was on the screen.
func (m *Model) rowLines() []string {
	h := m.viewHeight()
	bar := scrollbar(h, len(m.screen.rows), m.offset, m.styles)
	bar.Place(m.overviewMarks(bar)...)
	width := bodyWidth(m.diffWidth(), bar)

	out := make([]string, 0, h)
	m.lineRows = m.lineRows[:0]

	if head, ok := m.pinned(width); ok {
		out = append(out, head)
	}

	for i := m.offset; i < len(m.screen.rows) && len(out) < h; i++ {
		m.lineRows = rowsTo(m.lineRows, len(out), -1)

		if m.editingHere(i) {
			if m.answering() {
				out = append(out, m.answeredLines(i, h-len(out), width)...)
				i = m.spanEnd(i)

				continue
			}

			out = append(out, m.editorLines()...)
			i = m.spanEnd(i)

			continue
		}

		out = append(out, m.renderRow(i, width)...)
		m.lineRows = rowsTo(m.lineRows, len(out), i)

		if m.editingUnder(i) {
			out = append(out, m.editorLines()...)
		}
	}

	m.lineRows = rowsTo(m.lineRows, h, -1)[:h]

	for len(out) < h {
		out = append(out, "")
	}

	return alongside(out[:h], bar, m.diffWidth())
}

// rowsTo pads lines, the row each drawn line belongs to, out to n entries of row.
func rowsTo(lines []int, n, row int) []int {
	for len(lines) < n {
		lines = append(lines, row)
	}

	return lines
}

// cursorBar and rangeBar mark the first drawn line of the row under the cursor
// and of a row an open range covers. The band alone is too quiet a signal, and
// the thinner bar keeps the two apart where a palette quantizes them into one.
const (
	cursorBar = "▌"
	rangeBar  = "▏"
)

func (m *Model) renderRow(i, width int) []string {
	r := m.screen.rows[i]
	r.current = i == m.cursor

	bodies := m.rowBodies(r, width)
	if m.picking != nil && i == m.picking.row {
		bodies = m.labeled(bodies, r, width)
	}

	out := make([]string, len(bodies))

	for j, body := range bodies {
		mark := " "
		if j == 0 {
			switch {
			case i == m.cursor:
				mark = m.styles.cursor.Render(cursorBar)
			case m.inRange(i):
				mark = m.styles.selected.Render(rangeBar)
			}
		}

		out[j] = m.tinted(i, mark+body, width+1)
	}

	return out
}

// tinted draws a row's line in the face its place calls for: the cursor's, a
// covered range's, or none.
func (m *Model) tinted(i int, line string, width int) string {
	switch {
	case i == m.cursor:
		return frame.Lit(line, width, m.styles.row)
	case m.inRange(i):
		return frame.Lit(line, width, m.styles.span)
	}

	return line
}

// rowBodies is the row without the column that says where the cursor is, one
// answer per frame line it draws: every kind is one line except a code row
// under wrap, which is as many as its text takes. The rich renderer colors a
// line in pieces, so it hands back text already carrying its faces rather
// than one face for the whole row.
func (m *Model) rowBodies(r row, width int) []string {
	if r.kind == rowCode {
		if out := m.codeBody(r, width); out != nil {
			return out
		}
	}

	if r.role != 0 {
		return []string{m.cardLine(r, width)}
	}

	text, style := m.rowContent(r)
	if (r.kind == rowCode && m.behind(r)) || closed(r) {
		style = m.styles.behind
	}

	if len(r.spans) > 0 {
		return []string{m.spanLine(r, strings.TrimSuffix(text, r.text), style, width, closed(r))}
	}

	// A hunk is a boundary and carries the read mark whether or not it names
	// what encloses it, so it draws as a rule either way.
	if r.kind == rowFile || (r.kind == rowHunk && r.hunk > 0) {
		return []string{m.ruledFile(text, width, style)}
	}

	if r.lit != nil {
		return []string{cut(m.styles.note.Render(m.threadLead())+m.pastedCode(r), width)}
	}

	return []string{style.Render(cut(text, width))}
}

// codeBody picks the code renderer for the look the screen is in, and answers
// nil for the one look that falls through to the plain row path.
func (m *Model) codeBody(r row, width int) []string {
	switch {
	case m.sideBySide():
		return m.splitCode(r, width)
	case m.drawn.rich && r.mate.Kind != 0:
		return m.mergedCode(r, width)
	case m.drawn.rich:
		return m.richCode(r, width)
	case m.drawn.wrap:
		return m.plainWrapped(r, width)
	}

	return nil
}

// plainWrapped is a code row under the plain renderer with wrap on: the
// number and sign on the first line, and the continuation lines in the code
// column under them so the fold reads as one line.
func (m *Model) plainWrapped(r row, width int) []string {
	text, style := m.codeRow(r)
	if m.behind(r) {
		style = m.styles.behind
	}

	// The continuation's lead is the number, the sign, and the spaces between
	// them, so the fold lands in the code column rather than under the gutter.
	lead := strings.Repeat(" ", m.screen.numWidth+signRoom)

	var out []string

	for i, rest := 0, text; rest != ""; i++ {
		room := width
		if i > 0 {
			room -= textWidth(lead)
		}

		part, tail := takeCells(rest, max(1, room))
		if i > 0 {
			part = lead + part
		}

		out = append(out, style.Render(cut(part, width)))
		rest = tail
	}

	return out
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
			face = m.styles.cardNote
		}

		b.WriteString(under(face, m.styles.fill, r.role != 0).Render(r.text[piece.from:piece.to]))
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

// closed is a heading standing in for what it is hiding, which draws at the
// contrast of a hunk already read.
func closed(r row) bool { return r.folded && (r.kind == rowFile || r.kind == rowHunk) }

// ruledFile is a heading with a rule out to the frame's edge.
func (m *Model) ruledFile(text string, width int, style lipgloss.Style) string {
	return style.Render(cut(text, width)) + m.rule(textWidth(text), width)
}

func (m *Model) rowContent(r row) (string, lipgloss.Style) {
	switch r.kind {
	case rowBlank:
		return "", m.styles.footer
	case rowGroup, rowStop:
		return r.text, m.styles.title
	case rowFile:
		return "  " + m.readMark(r) + r.text, m.styles.file
	case rowHunk:
		return "    " + m.readMark(r) + r.text, m.styles.hunk
	case rowComment, rowNote, rowTurn:
		return m.commentRow(r)
	case rowMoved:
		return strings.Repeat(" ", m.screen.numWidth+1) + movedGlyph + " " + r.text, m.styles.hunk
	case rowGone:
		// It sits in the column the sign of a kept line sits in, so what came
		// out reads as part of the file rather than as something said about it.
		return strings.Repeat(" ", m.screen.numWidth+1) + "▁ " + r.text, m.styles.remove
	case rowThread:
		return m.threadRow(r)
	case rowTrouble:
		return m.troubleRow(r)
	case rowCode:
		return m.codeRow(r)
	case rowPeek, rowWhat:
		return r.text, m.styles.body
	case rowThen:
		return r.text, m.styles.note
	case rowShape:
		return m.shapeRow(r)
	case rowAsk:
		return m.askRow(r)
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
		return text, m.styles.cardNote
	case !r.head:
		return text, m.styles.body
	case r.comment < 0:
		// The review's own labels are the only headings inside the rail, so
		// they carry the weight that says a block starts here.
		return text, m.styles.rail.Bold(true)
	}

	return text, m.styles.forSeverity(m.review.Comments[r.comment].Severity)
}

// readMark says whether a hunk has been read. It is a glyph rather than a color
// so the one thing saying how much is left survives a monochrome terminal, and
// every heading spends the two cells whether or not it stands for a hunk, so a
// column of paths does not shift where one of them is read.
func (m *Model) readMark(r row) string {
	if r.hunk > 0 && m.read != nil && m.read.Has(m.hunkID(r.path, r.hunk)) {
		return "✓ "
	}

	return "  "
}

// threadRow renders a conversation already on GitHub. It shares the comment
// rail so it sits under its line the same way, and its prose reads at body
// contrast: a comment muted to a footnote's face is one nobody can read.
func (m *Model) threadRow(r row) (string, lipgloss.Style) {
	text := m.threadLead() + r.text
	if r.head {
		return text, m.styles.rail
	}

	return text, m.styles.body
}

func (m *Model) codeRow(r row) (string, lipgloss.Style) {
	number, style := r.line.New, m.styles.add

	switch r.line.Kind {
	case diff.KindRemove:
		number, style = r.line.Old, m.styles.remove
	case diff.KindContext:
		style = m.styles.context
	}

	glyph := sideGlyph(r.line.Kind, m.rich.colored)
	if r.whole {
		glyph = " "
	}

	text := fmt.Sprintf("%*d %s %s", m.screen.numWidth, number, glyph, expand(r.line.Text))

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

	// Truncate measures with the grapheme walk before deciding anything, so a
	// line that fits, which is nearly every one, is answered here instead.
	if textWidth(s) <= width {
		return s
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

// textWidth is the cells s takes on screen, escapes taking none.
func textWidth(s string) int {
	if w, ok := asciiWidth(s); ok {
		return w
	}

	return ansi.StringWidth(s)
}

// asciiWidth is textWidth for the strings that are printable ASCII and CSI
// escapes alone, which is most of what the screen measures: code under SGR
// colors. The ansi package segments every character into grapheme clusters,
// which was half of drawing a frame, and an ASCII character is always a
// cluster of its own one cell wide. Anything else answers false and is
// measured properly.
func asciiWidth(s string) (int, bool) {
	const esc, firstPrint, lastPrint = 0x1b, 0x20, 0x7e

	w := 0

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case c >= firstPrint && c <= lastPrint:
			w++
		case c == esc:
			end, ok := csiEnd(s, i)
			if !ok {
				return 0, false
			}

			i = end
		default:
			return 0, false
		}
	}

	return w, true
}

// csiEnd is the index of the final byte of the CSI sequence starting at i,
// in the strict form: parameters, then intermediates, then the final byte.
func csiEnd(s string, i int) (int, bool) {
	const (
		introducer = len("\x1b[")
		firstParam = 0x30
		lastParam  = 0x3f
		firstInter = 0x20
		lastInter  = 0x2f
		firstFinal = 0x40
		lastFinal  = 0x7e
	)

	if i+1 >= len(s) || s[i+1] != '[' {
		return 0, false
	}

	j := i + introducer
	for j < len(s) && s[j] >= firstParam && s[j] <= lastParam {
		j++
	}

	for j < len(s) && s[j] >= firstInter && s[j] <= lastInter {
		j++
	}

	if j >= len(s) || s[j] < firstFinal || s[j] > lastFinal {
		return 0, false
	}

	return j, true
}

// words is what the review is carrying, leaving out whatever it carries none
// of.
func (c tally) words() []string {
	var out []string

	for _, at := range [...]struct {
		n     int
		glyph string
		word  string
	}{{c.ready, "", "ready"}, {c.draft, "◐ ", draftWord}, {c.skip, "", "skipped"}, {c.todo, "", "todo"}} {
		if at.n > 0 {
			out = append(out, fmt.Sprintf("%s%d %s", at.glyph, at.n, at.word))
		}
	}

	return out
}
