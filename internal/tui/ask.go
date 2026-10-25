package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/acp"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/plan"
)

type askDraft struct {
	input   textinput.Model
	span    artifact.Span
	outside bool
}

type askAnsweredMsg struct {
	id   string
	text string
	err  error
}

type askCard struct {
	index int
	ask   *plan.Ask
}

func (m *Model) beginQuestion() tea.Cmd {
	if !m.onCode() && !m.onWholeLine() {
		m.say("A asks about a line of code, and this row is not one", false)

		return nil
	}

	r := m.screen.rows[m.cursor]
	start, line, side := m.takeRange(r.line, r.path)
	anchor, carried := m.diff.Anchor(r.path, side, line)

	m.drafting = &askDraft{
		input:   newPrompt(),
		span:    artifact.Span{Path: r.path, Side: side, StartLine: start, Line: line, Anchor: anchor},
		outside: !carried,
	}
	m.say("", false)

	return m.drafting.input.Focus()
}

func (m *Model) typeQuestion(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Accept):
		q := m.drafting
		m.drafting = nil
		cmd := m.ask(q.span, q.outside, strings.TrimSpace(q.input.Value()))

		return m, cmd
	case key.Matches(msg, m.keys.Back):
		m.drafting = nil
		m.say("nothing asked", false)

		return m, nil
	}

	var cmd tea.Cmd
	m.drafting.input, cmd = m.drafting.input.Update(msg)

	return m, cmd
}

func (m *Model) questionPrompt() string {
	s := m.drafting.span
	where := rangeWord(s.Path, firstOf(s.StartLine, s.Line), s.Line)
	head := m.styles.key.Render("A") + m.styles.footer.Render(" "+where+" › ")
	tail := m.styles.footer.Render("  enter asks · esc drops it")

	return cut(head+m.drafting.input.View()+tail, m.width)
}

// ask keeps the question in [plan] and puts it to whichever agent is here: the
// live session first, the one-shot asker next, and otherwise nobody, saying so.
func (m *Model) ask(span artifact.Span, outside bool, text string) tea.Cmd {
	if text == "" {
		m.say("nothing asked", false)

		return nil
	}

	id := m.nextAskID()
	m.review.Plan.Asks = append(m.review.Plan.Asks, artifact.PlanAsk{
		ID: id, Span: span, Question: text, Origin: artifact.OriginMe, Outside: outside,
	})
	m.replan()

	prompt := m.askPrompt(span, text)

	switch {
	case m.agentFree():
		m.inFlight[id] = m.agent.Name()
		m.save("asked " + m.agent.Name() + "; the answer lands on the card")
		m.openAgent()

		return tea.Batch(m.agentIn.Focus(), askSession(m, m.agent, id, prompt))
	case m.asker != nil:
		cmd := m.askOneShot(id, prompt)
		m.save("asked the agent; the answer lands on the card")

		return cmd
	}

	m.save("kept in [plan] for an agent to answer with second-look plan ask")

	return nil
}

// agentFree is whether the live session can take a question now.
func (m *Model) agentFree() bool { return m.agent != nil && m.agentDead == nil && !m.agent.Busy() }

// askOneShot puts the question to the configured asker and answers with an
// askAnsweredMsg under id.
func (m *Model) askOneShot(id, prompt string) tea.Cmd {
	m.inFlight[id] = "the agent"

	ask, ctx := m.asker, m.ctx

	return func() tea.Msg {
		answer, err := ask(ctx, prompt)

		return askAnsweredMsg{id: id, text: answer, err: err}
	}
}

// askSession prompts the live session and reads back what the agent said in
// that turn alone, which is every Agent entry after the ones already there.
func askSession(m *Model, s *acp.Session, id, prompt string) tea.Cmd {
	ctx := m.ctx

	return func() tea.Msg {
		before := len(s.Entries())

		_, err := s.Prompt(ctx, prompt)
		if err != nil {
			return askAnsweredMsg{id: id, err: err}
		}

		var said []string

		for _, e := range s.Entries()[before:] {
			if e.Kind == acp.Agent && strings.TrimSpace(e.Text) != "" {
				said = append(said, strings.TrimSpace(e.Text))
			}
		}

		return askAnsweredMsg{id: id, text: strings.Join(said, "\n\n")}
	}
}

// applyAnswered writes the answer into the ask, wherever the review has moved
// since it was asked. One the review no longer carries is said to and dropped.
func (m *Model) applyAnswered(msg askAnsweredMsg) {
	who := m.inFlight[msg.id]
	delete(m.inFlight, msg.id)

	if strings.HasPrefix(msg.id, replyPrefix) {
		m.applyDrafted(msg, who)

		return
	}

	at := slices.IndexFunc(m.review.Plan.Asks, func(a artifact.PlanAsk) bool { return a.ID == msg.id })

	switch {
	case at < 0:
		m.say("an answer came back for an ask that is gone", true)
	case msg.err != nil:
		m.failedAsks[msg.id] = msg.err.Error()
		m.rebuild()
		m.say("asking: "+msg.err.Error(), true)
	case strings.TrimSpace(msg.text) == "":
		m.failedAsks[msg.id] = "the agent said nothing"
		m.rebuild()
		m.say("the agent said nothing", false)
	default:
		m.review.Plan.Asks[at].Answer = strings.TrimSpace(msg.text)
		m.replan()
		m.save("answered")
	}
}

func (m *Model) nextAskID() string {
	for n := len(m.review.Plan.Asks) + 1; ; n++ {
		id := "ask-" + strconv.Itoa(n)
		if !slices.ContainsFunc(m.review.Plan.Asks, func(a artifact.PlanAsk) bool { return a.ID == id }) {
			return id
		}
	}
}

func (m *Model) askPrompt(s artifact.Span, text string) string {
	from := firstOf(s.StartLine, s.Line)

	lines, ok := m.rangeText(s.Path, s.Side, from, s.Line)
	if !ok {
		lines = m.headText(s.Path, from, s.Line)
	}

	return fmt.Sprintf(`A reviewer reading a change selected %s (the %s side of the diff) and asks:

%s

The lines:

%s

Answer in a few plain sentences, naming a file and line where it helps. Do not
edit anything.
`, rangeWord(s.Path, from, s.Line), strings.ToLower(s.Side), text, lines)
}

// onWholeLine is whether the cursor is on a line of a file opened whole, which
// an ask can be about even where the diff does not carry it.
func (m *Model) onWholeLine() bool {
	return m.view == viewFile && m.cursor < len(m.screen.rows) && m.screen.rows[m.cursor].kind == rowCode
}

// headText is lines from to through to of a file as it was read whole at head.
func (m *Model) headText(file string, from, to int) string {
	lines := m.blobs[file]
	if from < 1 || to > len(lines) || from > to {
		return ""
	}

	return strings.Join(lines[from-1:to], "\n")
}

func askCards(asks []plan.Ask) map[anchor][]askCard {
	out := map[anchor][]askCard{}

	for i := range asks {
		a := &asks[i]
		at := anchor{path: a.Path, side: a.Side, line: a.Line}
		out[at] = append(out[at], askCard{index: i, ask: a})
	}

	return out
}

func askRows(c askCard, lay layout, numWidth int) []row {
	a := c.ask
	avail := cardCols(lay.width, numWidth)
	r := func(text string, head bool) row {
		return row{kind: rowAsk, text: text, path: a.Path, comment: noComment, ask: c.index, head: head}
	}

	question := wrap(a.Question, avail)
	rows := make([]row, 0, len(question)+cardBorder)
	rows = append(rows, row{kind: rowBlank, path: a.Path, comment: noComment})
	head := r(a.Source.Glyph()+" "+sourceWord(a.Source)+" asked", true)
	head.aside = place(a.Path, a.Side, firstOf(a.StartLine, a.Line), a.Line)
	rows = append(rows, head)

	for _, line := range question {
		rows = append(rows, r(line, false))
	}

	rows = append(rows, r("", false))

	for _, line := range wrap(lay.answerWord(a), avail) {
		rows = append(rows, r(line, false))
	}

	return framed(rows, row{kind: rowAsk, path: a.Path, comment: noComment, ask: c.index}, lay.width)
}

func (l layout) answerWord(a *plan.Ask) string {
	switch who, waiting := l.inFlight[a.ID]; {
	case a.Answer != "":
		return a.Answer
	case waiting:
		return "asking " + who + "…"
	case l.failedAsks[a.ID] != "":
		return l.failedAsks[a.ID] + "; second-look plan ask can answer it"
	}

	return "no agent here to ask; second-look plan ask answers it"
}

func (m *Model) askRow(r row) (string, lipgloss.Style) {
	text := strings.Repeat(" ", m.screen.numWidth+indent) + "┃ " + r.text
	if r.head {
		return text, m.styles.rail
	}

	return text, m.styles.body
}

func isAsk(r row) bool { return r.head && r.kind == rowAsk }

func (m *Model) asksIn(c plan.Chapter) []int {
	var out []int

	for i := range m.plan.Asks {
		if slices.Contains(c.Hunks, m.plan.Asks[i].Ref) {
			out = append(out, i)
		}
	}

	return out
}
