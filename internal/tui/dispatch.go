package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/agents"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/brief"
)

const (
	setDirPerm  = 0o750
	setFilePerm = 0o600
)

// dispatch answers T: every comment handed back to an agent is written out as
// one set, and the agent is started on it where one is configured. A review
// already carrying a session hands that over too, so a second T is a follow-up
// rather than a restart.
//
// It is a separate key from S rather than a mode of it because the two are
// different acts. S blocks on a draft, because a draft is a comment nobody has
// ruled on and posting it would publish an unfinished thought. Handing work to
// an agent publishes nothing, so a draft elsewhere in the review is no reason
// to refuse.
func (m *Model) dispatch() tea.Cmd {
	owed := m.review.Todos()
	if len(owed) == 0 {
		m.say("nothing is marked todo; m then t hands a comment back", false)

		return nil
	}

	if m.store == "" {
		m.say("no store to write the set into", true)

		return nil
	}

	path := artifact.TodoPath(m.store, m.review.Number)

	if err := os.MkdirAll(filepath.Dir(path), setDirPerm); err != nil {
		m.say(fmt.Sprintf("writing the todo set: %v", err), true)

		return nil
	}

	if err := os.WriteFile(path, []byte(brief.Owed(m.review, m.diff, m.threads)), setFilePerm); err != nil {
		m.say(fmt.Sprintf("writing the todo set: %v", err), true)

		return nil
	}

	if m.dispatcher == nil {
		m.say(fmt.Sprintf("%s written to %s; nothing is configured to read it",
			plural(len(owed), "todo"), path), false)

		return nil
	}

	m.say(fmt.Sprintf("handing %s over…", plural(len(owed), "todo")), false)

	run, session := m.dispatcher, m.review.Agent.Session

	return func() tea.Msg {
		out, err := run(context.Background(), path, session)

		return dispatchedMsg{line: out, err: err}
	}
}

type dispatchedMsg struct {
	line string
	err  error
}

func (m *Model) dispatched(msg dispatchedMsg) {
	if msg.err != nil {
		m.say(fmt.Sprintf("dispatching: %v", msg.err), true)

		return
	}

	m.say(msg.line, false)
}

// probeAgent asks what the session recorded on the review is doing, behind the
// first frame like the other reads. A review with no session recorded asks
// nothing, the same as one with no listing configured.
func (m *Model) probeAgent() tea.Cmd {
	if m.agentProbe == nil || m.review.Agent.Session == "" {
		return nil
	}

	ask, session := m.agentProbe, m.review.Agent.Session

	return func() tea.Msg {
		state, err := ask(context.Background(), session)
		if err != nil {
			return nil
		}

		return agentStateMsg{state: state}
	}
}

// agentStateMsg is what the listing last said of the recorded session.
type agentStateMsg struct {
	state string
}

// agentWord is the fact the title carries about the session working the
// review: the tool's own state word, said loudly only when it is waiting on an
// answer.
func (m *Model) agentWord() string {
	switch m.agentState {
	case "":
		return ""
	case agents.Blocked:
		return m.styles.warn.Render("agent blocked")
	case agents.Done:
		return m.styles.ok.Render("agent done")
	default:
		return "agent " + m.agentState
	}
}
