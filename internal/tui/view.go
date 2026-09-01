package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

// tabStop is how wide a tab renders. A diff of Go or Python is unreadable if
// tabs reach the terminal, which advances the cursor without writing cells.
const tabStop = 4

// helpKeyWidth is the column the help overlay's descriptions start in.
const helpKeyWidth = 18

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
	right := cut(fmt.Sprintf("%s · %d ready · %d draft · %d skipped",
		m.progress(), c.ready, c.draft, c.skip), m.width)

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
	if m.status == "" {
		var b strings.Builder

		for _, k := range m.hints() {
			b.WriteString(" " + m.styles.key.Render(k[0]) + m.styles.footer.Render(" "+k[1]))
		}

		return []string{b.String()}
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

// hints is what is actionable where the cursor is standing. The keys that
// change a comment are shown on a comment and the keys that skip through the
// diff are shown on code, because both sets at once do not fit an 80-column
// frame and quitting has to be visible in either.
func (m *Model) hints() [][2]string {
	middle := [][2]string{{"n/p", "hunk"}, {"}/{", "file"}}

	switch {
	case m.currentThread() >= 0:
		middle = [][2]string{{"e", "reply"}}
	case m.current() >= 0:
		middle = [][2]string{{"r/d/x", "state"}, {"e", "edit"}}
	}

	hints := make([][2]string, 0, len(middle)+5)
	hints = append(hints, [2]string{"j/k", "line"}, [2]string{"tab", "comment"})
	hints = append(hints, middle...)

	return append(hints, [2]string{"S", "submit"}, [2]string{"?", "help"}, [2]string{"q", "quit"})
}

func (m *Model) helpLines() []string {
	out := make([]string, 0, m.viewHeight())

	for _, h := range helpLines() {
		out = append(out, "  "+m.styles.key.Render(pad(h[0], helpKeyWidth))+
			m.styles.footer.Render(cut(h[1], m.width-helpKeyWidth-indent)))
	}

	for len(out) < m.viewHeight() {
		out = append(out, "")
	}

	return out[:m.viewHeight()]
}

func (m *Model) rowLines() []string {
	h := m.viewHeight()
	out := make([]string, 0, h)

	for i := m.offset; i < m.offset+h; i++ {
		if i >= len(m.screen.rows) {
			out = append(out, "")

			continue
		}

		out = append(out, m.renderRow(i))
	}

	return out
}

func (m *Model) renderRow(i int) string {
	r := m.screen.rows[i]
	text, style := m.rowContent(r)
	text = cut(text, m.width)

	if i == m.cursor {
		return m.styles.cursor.Render(pad(text, m.width))
	}

	return style.Render(text)
}

func (m *Model) rowContent(r row) (string, lipgloss.Style) {
	switch r.kind {
	case rowBlank:
		return "", m.styles.footer
	case rowFile:
		return r.text, m.styles.file
	case rowHunk:
		return "  " + r.text, m.styles.hunk
	case rowComment:
		return m.commentRow(r)
	case rowThread:
		return m.threadRow(r)
	case rowCode:
		return m.codeRow(r)
	}

	return r.text, m.styles.body
}

func (m *Model) commentRow(r row) (string, lipgloss.Style) {
	text := strings.Repeat(" ", m.screen.numWidth+indent) + "│ " + r.text
	if !r.head {
		return text, m.styles.note
	}

	if r.comment < 0 {
		return text, m.styles.rail
	}

	return text, m.styles.forSeverity(m.review.Comments[r.comment].Severity)
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

func pad(s string, width int) string {
	if n := width - textWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}

	return s
}

func textWidth(s string) int { return ansi.StringWidth(s) }
