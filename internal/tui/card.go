package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/keyhint"
)

const (
	cornerCells = 6
	capCells    = 2
	asideGaps   = 2
)

// commentKeys and threadKeys are the keys a card's foot offers, most useful
// first, since a narrow card drops them from the end.
func commentKeys() [][2]string {
	return append(append([][2]string{{"e", "edit"}}, states()...), [2]string{"za", foldVerb})
}

func threadKeys() [][2]string {
	return [][2]string{{"e", replyWord}, {",", "react"}, {"za", foldVerb}}
}

func (m *Model) cardLine(r row, width int) string {
	inset := m.gutterWidth()
	cols := max(1, min(cardCols(m.diffWidth(), m.screen.numWidth), width-inset-cardBorder))
	lead := strings.Repeat(" ", inset)
	edge := m.cardEdge(r)

	switch r.role {
	case roleTop:
		return lead + m.topEdge(r, edge, cols+cardBorder)
	case roleFoot:
		return lead + m.footEdge(r, edge, cols+cardBorder)
	case roleFold:
		return lead + edge.Render("╶─ ") + m.cardTitle(r).Render(cut(r.text, cols)) + edge.Render(" ─╴")
	case roleBody:
	}

	content := m.on(m.bodyFace(r)).Render(cut(r.text, cols))
	if r.lit != nil {
		content = cut(m.pastedCode(r), cols)
	}

	side := m.on(edge).Render("│")

	return lead + side + m.filler(1) + content + m.filler(cols-textWidth(content)+1) + side
}

func (m *Model) topEdge(r row, edge lipgloss.Style, width int) string {
	room := width - cornerCells
	title := cut(r.text, room)
	used := textWidth(title)

	aside := ""
	if r.aside != "" && used+textWidth(r.aside)+cardBorder <= room {
		aside = r.aside
	}

	rule := room - used - 1
	if aside != "" {
		rule = room - used - textWidth(aside) - asideGaps
	}

	edge = m.on(edge)
	out := edge.Render("╭─") + m.filler(1) + m.on(m.cardTitle(r)).Render(title) + m.filler(1) +
		edge.Render(strings.Repeat("─", rule))

	if aside != "" {
		out += m.filler(1) + m.on(m.styles.hunk).Render(aside)
	}

	return out + m.filler(1) + edge.Render("─╮")
}

func (m *Model) footEdge(r row, edge lipgloss.Style, width int) string {
	keys := commentKeys()

	if r.kind == rowThread {
		keys = threadKeys()
	}

	if r.kind == rowPeek {
		keys = peekKeys()
	}

	if r.kind == rowAsk {
		keys = nil
	}

	face := keyhint.Styles{Key: m.on(m.styles.key), Text: m.on(m.styles.footer)}
	sep := m.on(edge).Render(" · ")
	room := width - cornerCells

	var line string

	for i, k := range keys {
		next := face.Key.Render("[" + k[0] + "]")
		if k[1] != "" {
			next = keyhint.One(face, keyhint.Hint{Key: k[0], What: k[1]})
		}

		if i > 0 {
			next = sep + next
		}

		if textWidth(line)+textWidth(next) > room {
			break
		}

		line += next
	}

	edge = m.on(edge)
	if line == "" {
		return edge.Render("╰" + strings.Repeat("─", width-capCells) + "╯")
	}

	return edge.Render("╰─") + m.filler(1) + line + m.filler(1) +
		edge.Render(strings.Repeat("─", room-textWidth(line))+"─╯")
}

func (m *Model) cardTitle(r row) lipgloss.Style {
	if r.kind == rowThread || r.kind == rowAsk || r.comment < 0 || r.comment >= len(m.review.Comments) {
		return m.styles.rail
	}

	return m.styles.forSeverity(m.review.Comments[r.comment].Severity)
}

func (m *Model) bodyFace(r row) lipgloss.Style {
	if r.kind == rowNote || r.kind == rowTurn {
		return m.styles.note
	}

	return m.styles.body
}

func (m *Model) cardEdge(r row) lipgloss.Style {
	switch {
	case m.cursorIn(r):
		return m.styles.selected
	case r.kind != rowThread && r.comment >= 0 && r.comment < len(m.review.Comments) &&
		agentOrigin(m.review.Comments[r.comment].Origin):
		return m.styles.agentEdge
	}

	return m.styles.edge
}

func (m *Model) cursorIn(r row) bool {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return false
	}

	here, ok := cardOf(m.screen.rows[m.cursor])
	there, other := cardOf(r)

	return ok && other && here == there
}

func cardOf(r row) (blockKey, bool) {
	switch {
	case r.role == 0:
		return blockKey{}, false
	case r.kind == rowThread:
		return blockKey{rowThread, r.thread}, true
	case r.kind == rowPeek:
		return blockKey{rowPeek, 0}, true
	case r.kind == rowAsk:
		return blockKey{rowAsk, r.ask}, true
	}

	return blockKey{rowComment, r.comment}, true
}

func (m *Model) on(s lipgloss.Style) lipgloss.Style { return s.Background(m.styles.fill) }

func (m *Model) filler(n int) string {
	return m.on(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(0, n)))
}
