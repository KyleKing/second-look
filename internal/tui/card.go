package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/keyhint"

	"github.com/kyleking/second-look/internal/artifact"
)

const (
	cornerCells = 6
	capCells    = 2
	asideGaps   = 2
)

// commentKeys is what a plain comment card's foot offers.
func commentKeys() [][2]string {
	return append(append([][2]string{{"e", editWord}}, states()...), [2]string{"za", foldVerb})
}

// verdictKeys are the keys that rule on a finding, in the order a foot lists them.
var verdictKeys = []struct {
	key, what string
	v         verdict
}{
	{"r", artifact.StatusReady, vReady},
	{"t", artifact.StatusTodo, vTodo},
	{"x", artifact.StatusSkip, vSkip},
	{"R", "resolve", vResolve},
}

// footKey is one key a card's foot offers: dim where it does nothing at the
// cursor, and lit where it is the ruling already made.
type footKey struct {
	hint keyhint.Hint
	lit  bool
}

func pairKeys(pairs [][2]string) []footKey {
	out := make([]footKey, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, footKey{hint: keyhint.Hint{Key: p[0], What: p[1]}})
	}

	return out
}

// rulingKeys is the foot of a card a verdict can be given on: reply and react
// bracket the four verdict keys, with the one standing lit.
func rulingKeys(lead [][2]string, standing verdict, off func(verdict) bool, tail [][2]string) []footKey {
	out := pairKeys(lead)

	for _, k := range verdictKeys {
		out = append(out, footKey{
			hint: keyhint.Hint{Key: k.key, What: k.what, Off: off(k.v)}, lit: standing == k.v,
		})
	}

	return append(out, pairKeys(tail)...)
}

// footKeys are the keys a card's foot offers, most useful first, since a narrow
// card drops them from the end.
func (m *Model) footKeys(r row) []footKey {
	switch {
	case r.kind == rowThread && r.thread < len(m.threads):
		th := &m.threads[r.thread]

		return rulingKeys([][2]string{{"e", replyWord}}, m.verdictOfThread(r.thread),
			func(v verdict) bool { return th.Resolved || (v == vResolve && th.ID == "") },
			[][2]string{{",", "react"}, {"za", foldVerb}})
	case r.kind == rowThread:
		return nil
	case r.kind == rowPeek && m.peeked != nil && m.peeked.graph != nil:
		return pairKeys(graphKeys())
	case r.kind == rowPeek:
		return pairKeys(peekKeys())
	case r.kind == rowAsk:
		return nil
	case r.comment >= 0 && r.comment < len(m.review.Comments) && m.review.Comments[r.comment].InReplyTo != 0:
		return rulingKeys([][2]string{{"e", editWord}}, verdictOf(&m.review.Comments[r.comment]),
			func(verdict) bool { return false }, [][2]string{{"za", foldVerb}})
	}

	return pairKeys(commentKeys())
}

// cardMeasure is the cells of text a card's body has in a frame width wide.
func (m *Model) cardMeasure(width int) int {
	return max(1, min(cardCols(m.diffWidth(), m.screen.numWidth), width-m.gutterWidth()-cardBorder))
}

func (m *Model) cardLine(r row, width int) string {
	inset := m.gutterWidth()
	cols := m.cardMeasure(width)
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

	state := ""
	if r.answer != "" {
		state = " · " + r.answer
	}

	title := cut(r.text, room-textWidth(state))
	used := textWidth(title) + textWidth(state)

	aside := ""
	if r.aside != "" && used+textWidth(r.aside)+cardBorder <= room {
		aside = r.aside
	}

	rule := room - used - 1
	if aside != "" {
		rule = room - used - textWidth(aside) - asideGaps
	}

	edge = m.on(edge)
	out := edge.Render("╭─") + m.filler(1) + m.on(m.cardTitle(r)).Render(title) + m.answerWord(r, state) +
		m.filler(1) + edge.Render(strings.Repeat("─", rule))

	if aside != "" {
		out += m.filler(1) + m.on(m.styles.hunk).Render(aside)
	}

	return out + m.filler(1) + edge.Render("─╮")
}

func (m *Model) answerWord(r row, state string) string {
	if state == "" {
		return ""
	}

	face := m.styles.priority
	if r.answered {
		face = m.styles.ok
	}

	return m.on(m.cardTitle(r)).Render(" · ") + m.on(face).Render(r.answer)
}

func (m *Model) footEdge(r row, edge lipgloss.Style, width int) string {
	face := keyhint.Styles{
		Key: m.on(m.styles.key), Text: m.on(m.styles.footer), Off: m.on(m.styles.behind),
	}
	lit := keyhint.Styles{Key: m.on(m.styles.selected), Text: m.on(m.styles.selected)}
	sep := m.on(edge).Render(" · ")
	room := width - cornerCells

	var line string

	for i, k := range m.footKeys(r) {
		next := keyhint.One(face, k.hint)

		switch {
		case k.hint.What == "":
			next = face.Key.Render("[" + k.hint.Key + "]")
		case k.lit:
			next = keyhint.One(lit, k.hint)
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
