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
	"github.com/kyleking/second-look/internal/artifact"
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

// WithDraftPrompt names what T asks an agent to do with an empty review
// instead of second-look's own default, for a config that already has a
// skill of its own to point the agent at by name.
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
)

// agentUpMsg is the session the adapter just opened, or why it would not.
type agentUpMsg struct {
	s    *acp.Session
	owed []artifact.Comment
	// send is a plain prompt to ask once the session is up, used in place of
	// owed when there was no todo set and the review was still empty.
	send string
	err  error
}

// agentWakeMsg is a streamed piece landing on the transcript; the pane reads
// Entries fresh, so the wake only has to say look again.
type agentWakeMsg struct{}

// agentTurnMsg is a prompt answered, with the adapter's word for how the turn
// stopped.
type agentTurnMsg struct {
	reason string
	err    error
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

// askAgent is T where a live session is the hand-off: an owed set goes over
// it as the prompt, an empty review with no session yet is asked to draft one
// from the diff instead, and otherwise an empty ask just opens the
// conversation.
func (m *Model) askAgent(owed []artifact.Comment) tea.Cmd {
	if m.agent != nil && m.agentDead != nil {
		m.agent.Close()
		m.agent, m.agentDead, m.agentSaw = nil, nil, false
	}

	send := m.draftSend(owed)

	if m.agent == nil {
		if len(owed) == 0 && m.review.Agent.Session == "" && send == "" {
			m.say("nothing is marked todo; m then t hands a comment back", false)

			return nil
		}

		start, load := m.agentStart, m.review.Agent.Session
		m.say("bringing the agent up…", false)

		return func() tea.Msg {
			s, err := start(context.Background(), load)

			return agentUpMsg{s: s, owed: owed, send: send, err: err}
		}
	}

	m.openAgent()

	switch {
	case len(owed) > 0:
		return tea.Batch(m.sendSet(m.agent, owed), watchAgent(m.agent))
	case send != "":
		return tea.Batch(m.sendText(m.agent, send), watchAgent(m.agent))
	}

	return watchAgent(m.agent)
}

// draftSend is the instruction T sends when nothing is owed, the review has
// not recorded a session yet, and nothing is staged on it either — the one
// time "hand back what's owed" and "there is nothing yet to review" are the
// same ask. A review already carrying a comment keeps the plain refusal
// instead: T hands back what is owed, it does not redo a review in progress.
func (m *Model) draftSend(owed []artifact.Comment) string {
	if len(owed) > 0 || len(m.review.Comments) > 0 || m.review.Agent.Session != "" {
		return ""
	}

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

// agentUp seats the session the adapter opened and asks it the todo set one
// was waiting on, or the draft instruction where there was no todo set but
// something to send anyway.
func (m *Model) agentUp(msg agentUpMsg) tea.Cmd {
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

	cmds := []tea.Cmd{watchAgent(msg.s)}

	switch {
	case len(msg.owed) > 0:
		cmds = append(cmds, m.sendSet(msg.s, msg.owed))
	case msg.send != "":
		cmds = append(cmds, m.sendText(msg.s, msg.send))
	}

	return tea.Batch(cmds...)
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

// sendSet writes the set where a hand-off always records it, then asks it of
// the session. A store that fails does not stop the ask: the file is the
// record, not the channel.
func (m *Model) sendSet(s *acp.Session, owed []artifact.Comment) tea.Cmd {
	if _, err := m.writeSet(); err != nil {
		m.say(err.Error(), true)
	} else {
		m.say(fmt.Sprintf("handing %s over", plural(len(owed), "todo")), false)
	}

	return m.askSession(s, m.setText())
}

// sendText asks the session a plain prompt with nothing written to disk
// first: the draft instruction records nothing itself, unlike a todo set,
// because what it asks for is a review the agent is about to stage on its own.
func (m *Model) sendText(s *acp.Session, text string) tea.Cmd {
	m.say(fmt.Sprintf("asking %s to draft the review…", s.Name()), false)

	return m.askSession(s, text)
}

// askSession asks text of the session and reports the turn back when it stops.
func (m *Model) askSession(s *acp.Session, text string) tea.Cmd {
	ctx := m.ctx

	return func() tea.Msg {
		reason, err := s.Prompt(ctx, text)

		return agentTurnMsg{reason: reason, err: err}
	}
}

// sendLine is enter in the pane: the typed line goes to the agent. A refused
// line — a turn running, the session ended — keeps the text rather than
// eating it.
func (m *Model) sendLine() tea.Cmd {
	text := strings.TrimSpace(m.agentIn.Value())
	if text == "" {
		return nil
	}

	switch {
	case m.agentDead != nil:
		m.say("the agent has ended; T starts it again", true)
	case m.agent.Busy():
		m.say("a turn is still running", false)
	default:
		m.agentIn.SetValue("")

		return m.askSession(m.agent, text)
	}

	return nil
}

// agentGone is the adapter ending: the transcript stays readable, and T
// spawns again over the recorded id.
func (m *Model) agentGone(err error) {
	m.agentDead = err
	if m.agentDead == nil {
		m.agentDead = errAgentEnded
	}

	m.say(fmt.Sprintf("%v", m.agentDead), errors.Is(err, errAgentEnded))
}

// agentTurned is a prompt answered. The transcript carries what streamed; the
// footer hears only a refusal or a crash.
func (m *Model) agentTurned(msg agentTurnMsg) {
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
		m.agentOpen = false
		m.agentIn.Blur()

		return m, nil
	case key.Matches(msg, m.keys.HalfUp):
		m.agentOff += m.paneHeight() / scrollHalf

		return m, nil
	case key.Matches(msg, m.keys.HalfDown):
		m.agentOff = max(0, m.agentOff-m.paneHeight()/scrollHalf)

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

// agentDivider is the pane's top rule: who is running, and how to leave.
func (m *Model) agentDivider() string {
	head := " " + m.agent.Name() + " — the agent has the keyboard; esc leaves, ctrl+\\ ends it"
	if m.agentDead != nil {
		head = " " + m.agent.Name() + " has ended — esc leaves, T starts it again"
	}

	rest := m.width - textWidth(head) - 1
	if rest >= ruleFloor {
		head += " " + strings.Repeat("─", rest)
	}

	return m.styles.hunk.Render(cut(head, m.width))
}

// agentRows is the transcript wrapped to the frame. An entry keeps its own
// voice: the reviewer on a rail marker, the agent plain, a thought in note
// italic, a tool call wearing its status.
func (m *Model) agentRows(width int) []string {
	var rows []string

	for _, e := range m.agent.Entries() {
		mark, face := m.entryFace(e)
		text := e.Text
		if e.Status != "" {
			text += " · " + e.Status
		}

		for _, line := range wrap(text, width-textWidth(mark)) {
			rows = append(rows, face.Render(cut(mark+line, width)))
		}
	}

	return rows
}

// entryFace is the gutter marker and style a transcript kind draws in.
func (m *Model) entryFace(e acp.Entry) (string, lipgloss.Style) {
	switch e.Kind {
	case acp.You:
		return "› ", m.styles.rail
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
