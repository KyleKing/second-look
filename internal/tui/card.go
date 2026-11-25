package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/keyhint"

	"github.com/kyleking/second-look/internal/artifact"
)

const (
	cornerCells = 6
	capCells    = 2
	asideGaps   = 2
)

// commentKeys is what a plain comment card's foot offers: rewrite it, the
// states, bare as a thread's verdicts are, and the fold. Every key is read from the keymap,
// so a foot follows a rebind or a move rather than naming a key that moved.
func (m *Model) commentKeys() []footKey {
	out := pairKeys([][2]string{{keyOf(m.keys.Edit), editWord}})
	for _, st := range []struct {
		b    key.Binding
		what string
	}{
		{m.keys.Ready, artifact.StatusReady},
		{m.keys.Draft, artifact.StatusDraft},
		{m.keys.Todo, artifact.StatusTodo},
		{m.keys.Skip, artifact.StatusSkip},
	} {
		out = append(out, footKey{hint: keyhint.Hint{Key: keyOf(st.b), What: st.what}})
	}

	return append(out, m.foldKey())
}

// foldKey is z then a, the fold every card answers.
func (m *Model) foldKey() footKey {
	return footKey{hint: keyhint.Hint{Key: keyOf(m.keys.Zed) + "a", What: foldVerb}}
}

type verdictKey struct {
	key, what string
	v         verdict
}

// verdictKeys are the keys that rule on a finding, in the order a foot lists them.
func (m *Model) verdictKeys() []verdictKey {
	return []verdictKey{
		{keyOf(m.keys.Ready), artifact.StatusReady, vReady},
		{keyOf(m.keys.Todo), artifact.StatusTodo, vTodo},
		{keyOf(m.keys.Skip), artifact.StatusSkip, vSkip},
		{keyOf(m.keys.Resolve), "resolve", vResolve},
	}
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

// rulingKeys is the foot of a card a verdict can be given on: the lead key and
// the tail bracket the four verdict keys, with the one standing lit.
func (m *Model) rulingKeys(lead footKey, standing verdict, off func(verdict) bool, tail ...footKey) []footKey {
	out := []footKey{lead}

	for _, k := range m.verdictKeys() {
		out = append(out, footKey{
			hint: keyhint.Hint{Key: k.key, What: k.what, Off: off(k.v)}, lit: standing == k.v,
		})
	}

	return append(out, tail...)
}

// footKeys are the keys a card's foot offers, most useful first, since a narrow
// card drops them from the end. A thread is answered with i, the family's
// insert; a staged reply in it is rewritten with e, which edits it in place.
func (m *Model) footKeys(r row) []footKey {
	switch {
	case r.kind == rowThread && r.thread < len(m.threads):
		th := &m.threads[r.thread]

		return m.rulingKeys(footKey{hint: keyhint.Hint{Key: keyOf(m.keys.Write), What: replyWord}},
			m.verdictOfThread(r.thread),
			func(v verdict) bool { return th.Resolved || (v == vResolve && th.ID == "") },
			footKey{hint: keyhint.Hint{Key: keyOf(m.keys.React), What: "react"}}, m.foldKey())
	case r.kind == rowThread:
		return nil
	case r.kind == rowPeek && m.peeked != nil && m.peeked.graph != nil:
		return pairKeys(graphKeys())
	case r.kind == rowPeek:
		return pairKeys(peekKeys())
	case r.kind == rowAsk:
		return nil
	case r.comment >= 0 && r.comment < len(m.review.Comments) && m.review.Comments[r.comment].InReplyTo != 0:
		return m.rulingKeys(footKey{hint: keyhint.Hint{Key: keyOf(m.keys.Edit), What: editWord}},
			verdictOf(&m.review.Comments[r.comment]), func(verdict) bool { return false }, m.foldKey())
	}

	return m.commentKeys()
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
		same := func(s lipgloss.Style) lipgloss.Style { return s }

		return lead + edge.Render("╶─ ") + m.spanText(r, m.cardTitle(r), cols, same) + edge.Render(" ─╴")
	case roleBody:
	}

	content := m.spanText(r, m.bodyFace(r), cols, m.on)
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

	// The title leaves a cell for the rule: filling room exactly gives strings.Repeat a negative count.
	title := cut(r.text, room-textWidth(state)-1)
	used := textWidth(title) + textWidth(state)

	aside := ""
	if r.aside != "" && used+textWidth(r.aside)+cardBorder <= room {
		aside = r.aside
	}

	rule := max(0, room-used-1)
	if aside != "" {
		rule = max(0, room-used-textWidth(aside)-asideGaps)
	}

	edge = m.on(edge)
	out := edge.Render("╭─") + m.filler(1) + m.spanText(r, m.cardTitle(r), textWidth(title), m.on) +
		m.answerWord(r, state) + m.filler(1) + edge.Render(strings.Repeat("─", rule))

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

// keysRow is the row the foot's hints describe: the row the cursor is on where
// it stands inside this card — including a reply nested in a thread's card —
// and the card's own foot anywhere else.
func (m *Model) keysRow(foot row) row {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return foot
	}

	cur := m.screen.rows[m.cursor]
	here, ok := cardOf(cur)
	own, isCard := cardOf(foot)

	switch {
	case ok && isCard && here == own:
		return cur
	case foot.kind == rowThread && foot.thread < len(m.threads) &&
		cur.comment >= 0 && cur.comment < len(m.review.Comments) &&
		m.review.Comments[cur.comment].InReplyTo == m.threads[foot.thread].ReplyTo():
		return cur
	}

	return foot
}

// footEdge is a card's bottom edge carrying its keys the way every footer in
// the family draws them: the key in the accent, its word quieter, no brackets.
func (m *Model) footEdge(r row, edge lipgloss.Style, width int) string {
	face := keyhint.Styles{
		Key: m.on(m.styles.hintKey), Text: m.on(m.styles.hintWord), Off: m.on(m.styles.behind), Bare: true,
	}
	lit := keyhint.Styles{Key: m.on(m.styles.selected), Text: m.on(m.styles.selected), Bare: true}
	sep := m.on(edge).Render(" · ")
	room := width - cornerCells

	var line string

	for i, k := range m.footKeys(m.keysRow(r)) {
		s := face
		if k.lit {
			s = lit
		}

		next := keyhint.One(s, k.hint)

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
		return edge.Render("╰" + strings.Repeat("─", max(0, width-capCells)) + "╯")
	}

	return edge.Render("╰─") + m.filler(1) + line + m.filler(1) +
		edge.Render(strings.Repeat("─", max(0, room-textWidth(line)))+"─╯")
}

func (m *Model) cardTitle(r row) lipgloss.Style {
	if r.kind == rowThread || r.kind == rowAsk || r.comment < 0 || r.comment >= len(m.review.Comments) {
		return m.styles.rail
	}

	return m.styles.forSeverity(m.review.Comments[r.comment].Severity)
}

// bodyFace is what a card's inner rows read in: notes and turns one step under
// the prose, and a reply's head at the severity a card of its own would carry.
func (m *Model) bodyFace(r row) lipgloss.Style {
	switch {
	case r.kind == rowNote || r.kind == rowTurn:
		return m.styles.cardNote
	case r.head:
		return m.cardTitle(r)
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
