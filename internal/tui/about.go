package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/ghmd"
	"github.com/kyleking/second-look/internal/threads"
)

// WithAbout carries what the pull request says about itself, cached by
// `second-look get`. Without it the screen shows the diff alone, which is what
// a review staged before this was fetched has.
func WithAbout(a threads.About) Option {
	return func(m *Model) { m.about = a }
}

// readAbout scrolls the overlay, folds and opens its collapsed sections, and
// closes it. Every other key closes it too: it changes nothing, so there is
// nothing to lose by leaving.
func (m *Model) readAbout(msg tea.KeyPressMsg) tea.Model {
	const halfPage = 2

	switch {
	case m.aboutZ:
		m.aboutZ = false
		switch msg.String() {
		case "a", "i":
			m.aboutExpand = !m.aboutExpand
		case "R":
			m.aboutExpand = true
		case "M", "c":
			m.aboutExpand = false
		}
	case msg.String() == "z":
		m.aboutZ = true
	case key.Matches(msg, m.keys.Down):
		m.aboutAt++
	case key.Matches(msg, m.keys.Up):
		m.aboutAt--
	case key.Matches(msg, m.keys.HalfDown):
		m.aboutAt += m.viewHeight() / halfPage
	case key.Matches(msg, m.keys.HalfUp):
		m.aboutAt -= m.viewHeight() / halfPage
	case key.Matches(msg, m.keys.Top):
		m.aboutAt = 0
	default:
		m.aboutOpen, m.aboutAt, m.aboutExpand = false, 0, false

		return m
	}

	m.aboutAt = clamp(m.aboutAt, max(0, len(m.aboutText(m.width))-m.viewHeight()))

	return m
}

func (m *Model) aboutLines() []string {
	h := m.viewHeight()
	text := m.aboutText(m.width)
	bar := scrollbar(h, len(text), m.aboutAt)

	out := text
	if m.aboutAt < len(out) {
		out = out[m.aboutAt:]
	}

	for len(out) < h {
		out = append(out, "")
	}

	return alongside(out[:h], bar, m.styles, m.width)
}

// aboutText is the overlay: what the change is called, who wrote it, how big it
// is, the description they wrote, and the comments left on the pull request
// rather than on a line of it. A coverage report, a preview build, and an
// agent's summary all arrive as those, and none of them is in the diff.
func (m *Model) aboutText(width int) []string {
	room := bodyWidth(width, nil) - indent

	out := []string{
		m.styles.title.Render(fmt.Sprintf(" %s/%s #%d",
			m.review.Owner, m.review.Repo, m.review.Number)),
	}

	if m.about.Title == "" {
		out = append(out, "", m.styles.note.Render(
			"  nothing was cached about this pull request; run second-look get "+
				strconv.Itoa(m.review.Number)+" to read it",
		))

		return append(out, m.planText(room)...)
	}

	out = append(out, " "+m.styles.file.Render(cut(m.about.Title, room)), " "+m.styles.note.Render(m.facts2()))

	if body := strings.TrimSpace(m.about.Body); body != "" {
		out = append(out, "")
		out = append(out, m.mdLines(body, room)...)
	}

	for i := range m.about.Comments {
		c := &m.about.Comments[i]
		out = append(out, "", " "+m.styles.head.Render("@"+c.Author))
		out = append(out, m.mdLines(c.Body, room)...)
	}

	return append(out, m.planText(room)...)
}

// planText is what the reading order was cut from and every placement it
// could not honor, which the header only counts.
func (m *Model) planText(room int) []string {
	out := []string{"", " " + m.styles.head.Render("reading plan")}

	for _, line := range wrap(m.plan.Basis, room) {
		out = append(out, "  "+m.styles.note.Render(line))
	}

	for _, p := range m.plan.Problems {
		for i, line := range wrap(p, room-indent) {
			lead := "  ⚠ "
			if i > 0 {
				lead = "    "
			}

			out = append(out, lead+m.styles.warn.Render(line))
		}
	}

	return out
}

// facts2 is who wrote it, how much it changes, and what it is labeled.
func (m *Model) facts2() string {
	parts := make([]string, 0, 3)

	if m.about.Author != "" {
		parts = append(parts, m.about.Author)
	}

	if m.about.Added > 0 || m.about.Removed > 0 {
		parts = append(parts, fmt.Sprintf("+%d -%d", m.about.Added, m.about.Removed))
	}

	if len(m.about.Labels) > 0 {
		parts = append(parts, strings.Join(m.about.Labels, ", "))
	}

	return "  " + strings.Join(parts, " · ")
}

// mdLines draws a GitHub comment body as far as a screen can: prose wrapped,
// code and tables left as written, and a collapsed section shown by its summary
// alone, which is what it is collapsed for.
func (m *Model) mdLines(body string, room int) []string {
	var out []string

	for i, b := range ghmd.Parse(body) {
		if i > 0 {
			out = append(out, "")
		}

		out = append(out, m.mdBlock(&b, room, "  ")...)
	}

	return out
}

func (m *Model) mdBlock(b *ghmd.Block, room int, lead string) []string {
	switch b.Kind {
	case ghmd.Code:
		if b.Lang == "mermaid" && !m.aboutExpand {
			return []string{lead + m.styles.note.Render(
				cut("▸ mermaid diagram · "+plural(len(b.Lines), "line"), room))}
		}

		return flatLines(b.Lines, room, lead+"  ", m.styles.body)
	case ghmd.Table:
		return flatLines(tableLines(b.Lines, room-len(lead)), room, lead, m.styles.note)
	case ghmd.Rule:
		return []string{lead + m.styles.note.Render(strings.Repeat("─", min(room, ruleWidth)))}
	case ghmd.Details:
		return m.detailsLines(b, room, lead)
	case ghmd.Quote, ghmd.Prose:
	}

	return flatLines(wrap(strings.Join(b.Lines, "\n"), room-len(lead)), room, lead, m.styles.body)
}

// detailsLines draws a collapsed section by its summary and how much it holds,
// or by the blocks themselves where z R opened them. The fold is the same za,
// z R, z M a note's own sections take.
func (m *Model) detailsLines(b *ghmd.Block, room int, lead string) []string {
	label := arrow(m.aboutExpand) + " " + summaryOf(b) + " · " + plural(hiddenLines(b), "line")
	out := []string{lead + m.styles.note.Render(cut(label, room))}

	if !m.aboutExpand {
		return out
	}

	for i := range b.Blocks {
		out = append(out, m.mdBlock(&b.Blocks[i], room, lead+"  ")...)
	}

	return out
}

// hiddenLines counts what a collapsed section would draw opened, so the fold
// says what it costs to open.
func hiddenLines(b *ghmd.Block) int {
	n := len(b.Lines)
	for i := range b.Blocks {
		n += hiddenLines(&b.Blocks[i])
	}

	return n
}

func flatLines(lines []string, room int, lead string, style lipgloss.Style) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, lead+style.Render(cut(l, room-len(lead))))
	}

	return out
}

func summaryOf(b *ghmd.Block) string {
	if b.Summary != "" {
		return b.Summary
	}

	return "collapsed section"
}
