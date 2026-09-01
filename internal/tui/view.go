package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
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
	right := cut(fmt.Sprintf("%s · %s · %s%s%d ready · %d draft · %d skipped",
		m.progress(), m.treeWord(), m.costCount(), m.readCount(), c.ready, c.draft, c.skip), m.width)

	if word := m.view.String(); word != "" {
		left += "  " + word
	}

	if path := m.rowPath(); path != "" {
		left += "  " + path
	}

	// The counts and the position are fixed width, so the path yields to them
	// rather than pushing the line past the frame.
	left = cut(left, max(1, m.width-textWidth(right)-1))
	gap := max(1, m.width-textWidth(left)-textWidth(right))

	return m.styles.title.Render(left) +
		strings.Repeat(" ", gap) + m.styles.subtitle.Render(right)
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

// progress is how far through the review the cursor is, which a frame with no
// scrollbar otherwise cannot say. It is fixed width so the title does not
// shift under the eye on every keystroke.
func (m *Model) progress() string {
	last := len(m.screen.rows) - 1
	if last < 1 {
		return "100%"
	}

	return fmt.Sprintf("%3d%%", m.cursor*100/last)
}

// tally is how many comments will post, how many block the post, and how many
// were considered and declined.
type tally struct {
	ready int
	draft int
	skip  int
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
		}
	}

	return out
}

func (m *Model) rowPath() string {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return ""
	}

	return m.screen.rows[m.cursor].path
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
	out := make([]string, 0, h)

	for i := m.offset; i < len(m.screen.rows) && len(out) < h; i++ {
		if m.editingHere(i) {
			out = append(out, m.editorLines()...)
			i = m.spanEnd(i)

			continue
		}

		out = append(out, m.renderRow(i))
	}

	for len(out) < h {
		out = append(out, "")
	}

	return out[:h]
}

// cursorBar is the column every row spends on saying whether the cursor is on
// it. It is a glyph rather than a reversed row, so a wide terminal does not
// answer a keystroke with a bar of inverted text across it.
const cursorBar = "▌"

func (m *Model) renderRow(i int) string {
	r := m.screen.rows[i]
	text, style := m.rowContent(r)
	text = cut(text, m.width-1)

	if i == m.cursor {
		return m.styles.cursor.Render(cursorBar) + style.Render(text)
	}

	return " " + style.Render(text)
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
	case rowComment, rowNote:
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
	case r.kind == rowNote:
		return text, m.styles.note
	case !r.head:
		return text, m.styles.body
	case r.comment < 0:
		return text, m.styles.rail
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
	text := strings.Repeat(" ", m.screen.numWidth+indent) + "│ " + r.text
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
