package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

	return strings.Join(append(append([]string{m.title()}, body...), m.footer()), "\n")
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
	left = cut(left, max(1, m.width-runeLen(right)-1))
	gap := max(1, m.width-runeLen(left)-runeLen(right))

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

func (m *Model) footer() string {
	if m.status != "" {
		style := m.styles.footer
		if m.failed {
			style = m.styles.fail
		}

		return style.Render(cut(m.status, m.width))
	}

	var b strings.Builder

	for _, k := range [][2]string{
		{"n/p", "hunk"},
		{"}/{", "file"},
		{"tab", "comment"},
		{"e", "edit"},
		{"r/d/x", "state"},
		{"S", "submit"},
		{"?", "help"},
	} {
		b.WriteString(" " + m.styles.key.Render(k[0]) + m.styles.footer.Render(" "+k[1]))
	}

	return b.String()
}

func (m *Model) helpLines() []string {
	out := make([]string, 0, m.viewHeight())

	for _, h := range helpLines() {
		out = append(out, "  "+m.styles.key.Render(pad(h[0], helpKeyWidth))+m.styles.footer.Render(h[1]))
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

func cut(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}

	if width < 1 {
		return ""
	}

	return string(runes[:width-1]) + "…"
}

func pad(s string, width int) string {
	if n := width - runeLen(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}

	return s
}

func runeLen(s string) int { return len([]rune(s)) }
