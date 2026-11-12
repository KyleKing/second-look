package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/acp"
)

// AgentStarter opens an agent session over ACP, loading the one the review
// records where the agent supports resuming it. Which adapter the argv starts
// is the caller's config, not the screen's.
type AgentStarter func(ctx context.Context, loadID string) (*acp.Session, error)

// WithAgent lets T open a live session over the todo set rather than run the
// dispatch command: the transcript sits in a pane under the diff and enter
// talks to it. Where no agent is configured the dispatcher stays the hand-off.
func WithAgent(start AgentStarter) Option {
	return func(m *Model) { m.agentStart = start }
}

// WithDraftPrompt names what the pane's hand-off key asks an agent to do with
// an empty review instead of second-look's own default, for a config that
// already has a skill of its own to point the agent at by name.
func WithDraftPrompt(prompt string) Option {
	return func(m *Model) { m.draftText = prompt }
}

// errAgentEnded marks a session that stopped on its own or on ctrl+\, so the
// title can say ended rather than repeat a nil error as silence.
var errAgentEnded = errors.New("the agent ended")

const (
	// Pane fraction ctrl+u/d moves the transcript by.
	scrollHalf = 2
	// What a transcript line yields to the frame's edge.
	agentPad = 2
	// How far a tool call's output sits under the call's own line.
	detailIndent = 4
)

// agentUpMsg is the session the adapter just opened, or why it would not.
type agentUpMsg struct {
	s   *acp.Session
	err error
}

// agentWakeMsg is a streamed piece landing on the transcript; the pane reads
// Entries fresh, so the wake only has to say look again.
type agentWakeMsg struct{}

// agentTurnMsg is a prompt answered, with the adapter's word for how the turn
// stopped and the text that was sent so a refused one can be queued again.
type agentTurnMsg struct {
	reason string
	err    error
	sent   string
}

// agentGoneMsg is the adapter ending, with its stderr where it did not end
// cleanly.
type agentGoneMsg struct{ err error }

// watchAgent wakes the pane when a streamed piece lands or the adapter ends.
func watchAgent(s *acp.Session) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-s.Changed():
			return agentWakeMsg{}
		case err := <-s.Dead():
			return agentGoneMsg{err: err}
		}
	}
}

// askAgent is T where a live session is the hand-off: it brings the pane up
// over the agent and leaves the first word to the person. The todo set and
// the draft instruction go on the pane's own key (ctrl+t) rather than firing
// on open, because a key that also spends a turn is a key that cannot be
// pressed just to look.
func (m *Model) askAgent() tea.Cmd {
	if m.agent != nil && m.agentDead != nil {
		m.agent.Close()
		m.agent, m.agentDead, m.agentSaw = nil, nil, false
	}

	if m.agent == nil {
		if m.agentLoading {
			return nil
		}

		start, load := m.agentStart, m.review.Agent.Session
		m.agentLoading = true
		m.say("bringing the agent up…", false)

		return func() tea.Msg {
			s, err := start(context.Background(), load)

			return agentUpMsg{s: s, err: err}
		}
	}

	m.openAgent()

	return watchAgent(m.agent)
}

// agentDraft is the instruction ctrl+t sends on a review with nothing staged
// and nothing owed: the agent's own where the config names one, second-look's
// default otherwise. A review already carrying a comment keeps the plain
// refusal instead: the key hands back what is owed, it does not redo a review
// in progress.
func (m *Model) agentDraft() string {
	if m.draftText != "" {
		return m.draftText
	}

	return defaultDraftPrompt(m.review.Owner, m.review.Repo, m.review.Number)
}

// defaultDraftPrompt is what T sends an agent when no [[agent]] names its own
// draft prompt. It points at second-look's own skill rather than naming a
// private one, since that is the contract every adapter can follow without
// this project knowing what any particular agent calls its own wrapper.
func defaultDraftPrompt(owner, repo string, number int) string {
	return fmt.Sprintf(
		"Review %s/%s#%d. Run `second-look skill` for the contract: read the diff with "+
			"`second-look get`, stage findings as draft comments with `second-look comment add`, "+
			"and write the review's own note saying what you read and ran. Everything you stage "+
			"is a draft for the person at this keyboard to read before anything posts.",
		owner, repo, number)
}

// agentUp seats the session the adapter opened and drains anything staged
// while there was none to ask.
func (m *Model) agentUp(msg agentUpMsg) tea.Cmd {
	m.agentLoading = false

	if msg.err != nil {
		m.say(fmt.Sprintf("starting the agent: %v", msg.err), true)

		return nil
	}

	m.agent = msg.s
	m.openAgent()
	m.j.Event("review", "agent "+msg.s.Name())

	if id := msg.s.ID(); id != m.review.Agent.Session {
		m.review.Agent.Session = id
		m.save(msg.s.Name() + " is listening")
	} else {
		m.say(msg.s.Name()+" is listening", false)
	}

	return tea.Batch(watchAgent(msg.s), m.pumpAgent())
}

// openAgent brings the pane up over the session, scrolled to its end, and
// focuses the prompt: a pane reopened after esc takes a line the same as one
// just started, and a fresh input listens to nothing until it is focused.
func (m *Model) openAgent() {
	m.agentOpen = true
	m.agentOff = 0
	m.agentIn = newPrompt()
	m.agentIn.Prompt = "❯ "
	m.agentIn.Placeholder = "talk to " + m.agent.Name()
	// The command Focus answers is a cursor blink, which newPrompt turns off.
	m.agentIn.Focus()
	m.showing = nil
}

// agentHandoff is ctrl+t in the pane: the owed set goes over as the prompt,
// with the set file written where a hand-off always records it; a review with
// nothing staged and nothing owed is asked to draft from the diff instead.
// A store that fails does not stop the ask: the file is the record, not the
// channel.
func (m *Model) agentHandoff() tea.Cmd {
	switch owed := m.review.Todos(); {
	case len(owed) > 0:
		if _, err := m.writeSet(); err != nil {
			m.say(err.Error(), true)

			return nil
		}

		m.say(fmt.Sprintf("handing %s over", plural(len(owed), "todo")), false)

		return m.enqueueAgent(m.setText())
	case len(m.review.Comments) == 0:
		m.say(fmt.Sprintf("asking %s to draft the review…", m.agent.Name()), false)

		return m.enqueueAgent(m.agentDraft())
	default:
		m.say("nothing is owed; m then t marks a comment first", false)

		return nil
	}
}

// enqueueAgent stages a message for the session. The queue is the shape a
// running turn takes: nothing typed is eaten, and it sends when the agent is
// next free rather than when it was typed.
func (m *Model) enqueueAgent(text string) tea.Cmd {
	m.agentQueue = append(m.agentQueue, text)

	return m.pumpAgent()
}

// pumpAgent puts the head of the queue on the wire where the session can take
// it; the agentSending flag marks one dispatched so a second pump cannot
// start a parallel turn before Prompt marks the session busy.
func (m *Model) pumpAgent() tea.Cmd {
	if m.agent == nil || m.agentDead != nil || m.agentSending ||
		len(m.agentQueue) == 0 || m.agent.Busy() {
		return nil
	}

	m.agentSending = true
	text := m.agentQueue[0]
	m.agentQueue = m.agentQueue[1:]

	return m.askSession(m.agent, text)
}

// askSession asks text of the session and reports the turn back when it stops.
func (m *Model) askSession(s *acp.Session, text string) tea.Cmd {
	ctx := m.ctx

	return func() tea.Msg {
		reason, err := s.Prompt(ctx, text)

		return agentTurnMsg{reason: reason, err: err, sent: text}
	}
}

// sendLine is enter in the pane: the typed line stages for the agent, sending
// now where it is free or when the turn in flight ends.
func (m *Model) sendLine() tea.Cmd {
	text := strings.TrimSpace(m.agentIn.Value())
	if text == "" {
		return nil
	}

	if m.agentDead != nil {
		m.say("the agent has ended; T starts it again", true)

		return nil
	}

	m.agentIn.SetValue("")

	return m.enqueueAgent(text)
}

// agentGone is the adapter ending: the transcript stays readable, the queue
// stays staged, and T spawns again over the recorded id.
func (m *Model) agentGone(err error) {
	m.agentDead = err
	m.agentSending = false
	if m.agentDead == nil {
		m.agentDead = errAgentEnded
	}

	m.say(fmt.Sprintf("%v", m.agentDead), errors.Is(err, errAgentEnded))
}

// agentTurned is a prompt answered. The transcript carries what streamed; the
// footer hears only a refusal or a crash. A refused send — the queue raced a
// turn it thought was over — goes back on the queue rather than being lost.
func (m *Model) agentTurned(msg agentTurnMsg) {
	m.agentSending = false

	if errors.Is(msg.err, acp.ErrBusy) {
		m.agentQueue = append([]string{msg.sent}, m.agentQueue...)

		return
	}

	m.agentSaw = true

	switch {
	case msg.err != nil:
		m.say(fmt.Sprintf("the turn ended badly: %v", msg.err), true)
	case msg.reason != "" && msg.reason != "end_turn":
		m.say("the turn stopped: "+msg.reason, true)
	}
}

// agentKey is the keyboard while the pane is open. A pending ask owns it
// first, the same way a confirmation does: a digit picks its option and esc
// declines, and nothing else reaches the prompt while the agent waits.
// Elsewhere esc peels one layer at a time — staged messages first, the pane
// itself only once nothing is staged — and ctrl+w drops one queued message,
// but only on an empty input so a typed word still deletes a word.
func (m *Model) agentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if p := m.agent.Pending(); p != nil {
		m.answerPermit(p, msg)

		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.EndPane):
		m.agent.Close()

		return m, nil
	case key.Matches(msg, m.keys.Back):
		if n := len(m.agentQueue); n > 0 {
			m.agentQueue = nil
			m.say("dropped "+plural(n, "queued message"), false)
		} else {
			m.agentOpen = false
			m.agentIn.Blur()
		}

		return m, nil
	case key.Matches(msg, m.keys.SendSet):
		cmd := m.agentHandoff()

		return m, cmd
	case key.Matches(msg, m.keys.Expand):
		m.agentExpand = !m.agentExpand

		return m, nil
	case key.Matches(msg, m.keys.HalfUp):
		m.agentOff += m.paneHeight() / scrollHalf

		return m, nil
	case key.Matches(msg, m.keys.HalfDown):
		m.agentOff = max(0, m.agentOff-m.paneHeight()/scrollHalf)

		return m, nil
	case key.Matches(msg, m.keys.DropQueued) && m.agentIn.Value() == "":
		if n := len(m.agentQueue); n > 0 {
			m.agentQueue = m.agentQueue[:n-1]
		}

		return m, nil
	case key.Matches(msg, m.keys.Accept):
		cmd := m.sendLine()

		return m, cmd
	default:
		var cmd tea.Cmd
		m.agentIn, cmd = m.agentIn.Update(msg)

		return m, cmd
	}
}

// answerPermit is the keyboard while an ask is up: a digit picks its option,
// esc declines. Both are answers every adapter can take.
func (m *Model) answerPermit(p *acp.Permit, msg tea.KeyPressMsg) {
	switch {
	case key.Matches(msg, m.keys.Back):
		p.Decline()
	case len(msg.Text) == 1 && msg.Text[0] >= '1' && int(msg.Text[0]-'0') <= len(p.Options):
		p.Answer(p.Options[msg.Text[0]-'1'].ID)
	}
}

// agentLines draws the session pane: a divider naming the agent, the
// transcript's tail, and the prompt line a pending ask stands in for.
func (m *Model) agentLines() []string {
	room := m.paneHeight() - 1

	rows := m.agentRows(max(1, m.width-agentPad))
	hi := max(0, len(rows)-m.agentOff)
	lo := max(0, hi-room)
	visible := rows[lo:hi]

	out := []string{m.agentDivider()}
	out = append(out, visible...)
	for len(out) < room+1 {
		out = append(out, "")
	}

	return append(out, m.agentAsk())
}

// agentDivider is the pane's top rule: who is running, in what mode, and how
// to leave — with the hand-off key named while there is something to hand.
func (m *Model) agentDivider() string {
	head := " " + m.agent.Name()
	if mode := m.agent.Mode(); mode != "" {
		head += " · " + mode
	}

	switch {
	case m.agentDead != nil:
		head += " has ended — esc leaves, T starts it again"
	default:
		head += " — esc leaves · ctrl+\\ ends"
		if n := len(m.review.Todos()); n > 0 {
			head += " · ctrl+t hands " + plural(n, "todo")
		}
	}

	if n := len(m.agentQueue); n > 0 {
		head += " · " + plural(n, "queued")
	}

	rest := m.width - textWidth(head) - 1
	if rest >= ruleFloor {
		head += " " + strings.Repeat("─", rest)
	}

	return m.styles.hunk.Render(cut(head, m.width))
}

// agentRows is the transcript wrapped to the frame. An entry keeps its own
// voice: the reviewer on a rail marker, the agent plain, a thought in note
// italic, a tool call wearing its status. A run of calls draws as one line —
// the detail of what ran is noise next to what the agent said — and ctrl+o
// unfolds it into a line a call with each call's output. A turn's end is a
// rule so scrolling back can tell where one answer stopped and the next
// prompt began.
func (m *Model) agentRows(width int) []string {
	var rows []string

	var tools []acp.Entry

	flush := func() {
		rows = append(rows, m.toolRunRows(tools, width)...)
		tools = nil
	}

	entries := m.agent.Entries()
	for i, e := range entries {
		if e.Kind == acp.Tool {
			tools = append(tools, e)

			continue
		}

		flush()

		if e.Kind == acp.You && i > 0 {
			rows = append(rows, m.styles.hunk.Render(strings.Repeat("─", width)))
		}

		rows = append(rows, m.entryRows(e, width)...)
	}
	flush()

	switch {
	case m.agent.Busy() || m.agentSending:
		rows = append(rows, m.styles.note.Render(cut("· working…", width)))
	case m.agentSaw:
		rows = append(rows, m.styles.note.Render(cut("· done", width)))
	}

	for _, q := range m.agentQueue {
		first, _, _ := strings.Cut(q, "\n")
		rows = append(rows, m.styles.note.Render(cut("  queued › "+first, width)))
	}

	return rows
}

// entryRows draws one transcript entry, marked and faced for its kind.
func (m *Model) entryRows(e acp.Entry, width int) []string {
	mark, face := m.entryFace(e)
	text := e.Text
	if e.Status != "" {
		text += " · " + e.Status
	}

	rows := make([]string, 0, strings.Count(text, "\n")+1)
	for _, line := range wrap(text, width-textWidth(mark)) {
		rows = append(rows, face.Render(cut(mark+line, width)))
	}

	return rows
}

// toolRunRows draws one run of consecutive tool calls: each on a line of its
// own when expanded — with the output it returned underneath — or folded to
// a count when not, so a long turn of calls stays one line of the transcript.
func (m *Model) toolRunRows(run []acp.Entry, width int) []string {
	if len(run) == 0 {
		return nil
	}

	if m.agentExpand {
		var rows []string
		for _, e := range run {
			rows = append(rows, m.entryRows(e, width)...)
			for _, line := range wrap(e.Detail, width-detailIndent) {
				rows = append(rows, m.styles.note.Render(cut(strings.Repeat(" ", detailIndent)+line, width)))
			}
		}

		return rows
	}

	if len(run) == 1 {
		return m.entryRows(run[0], width)
	}

	line := fmt.Sprintf("· %d tool calls", len(run))
	if last := run[len(run)-1]; last.Status != "" {
		line += " · " + last.Status
	}

	line += " · ctrl+o unfolds"

	return []string{m.styles.hunk.Render(cut(line, width))}
}

// entryFace is the gutter marker and style a transcript kind draws in.
func (m *Model) entryFace(e acp.Entry) (string, lipgloss.Style) {
	switch e.Kind {
	case acp.You:
		return "› ", m.styles.rail
	case acp.Agent:
		return "● ", m.styles.body
	case acp.Thought:
		return "  ", m.styles.note
	case acp.Tool:
		return "· ", m.styles.hunk
	case acp.Plan:
		return "≡ ", m.styles.hunk
	case acp.Note:
		return "! ", m.styles.warn
	default:
		return "  ", m.styles.body
	}
}

// agentAsk is the pane's bottom row: a pending ask's numbered options, or the
// prompt where a line goes to the agent.
func (m *Model) agentAsk() string {
	if p := m.agent.Pending(); p != nil {
		opts := make([]string, len(p.Options))
		for i, o := range p.Options {
			opts[i] = fmt.Sprintf("%d %s", i+1, o.Name)
		}

		title := p.Title
		if title == "" {
			title = p.Tool
		}

		return m.styles.warn.Render(cut(" "+title+" — "+strings.Join(opts, " · ")+" · esc declines", m.width))
	}

	if m.agentDead != nil {
		return m.styles.footer.Render("  the agent has ended")
	}

	// The input has no width of its own until one is set, and an unset width
	// collapses the placeholder to its first letter.
	m.agentIn.SetWidth(max(1, m.width-agentPad))

	return m.agentIn.View()
}
