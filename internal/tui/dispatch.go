package tui

import (
	"context"
	"errors"
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

	if m.agentStart != nil {
		return m.askAgent(owed)
	}

	if len(owed) == 0 {
		m.say("nothing is marked todo; m then t hands a comment back", false)

		return nil
	}

	path, err := m.writeSet()
	if err != nil {
		m.say(err.Error(), true)

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

// errNoStore is a todo set with nowhere to be written: the review came up
// without a cache directory.
var errNoStore = errors.New("no store to write the set into")

// setText is the set as markdown, the artifact's own text.
func (m *Model) setText() string {
	return brief.Owed(m.review, m.diff, m.threads)
}

// writeSet leaves the todo set where a hand-off always records it.
func (m *Model) writeSet() (string, error) {
	if m.store == "" {
		return "", errNoStore
	}

	path := artifact.TodoPath(m.store, m.review.Number)

	if err := os.MkdirAll(filepath.Dir(path), setDirPerm); err != nil {
		return "", fmt.Errorf("writing the todo set: %w", err)
	}

	if err := os.WriteFile(path, []byte(m.setText()), setFilePerm); err != nil {
		return "", fmt.Errorf("writing the todo set: %w", err)
	}

	return path, nil
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
// review. A live one knows more than the listing: an open ask is the agent
// waiting on a person, a running turn is working.
func (m *Model) agentWord() string {
	if m.agent != nil {
		switch {
		case m.agentDead != nil:
			return m.styles.warn.Render("agent ended")
		case m.agent.Pending() != nil:
			return m.styles.warn.Render("agent waiting")
		case m.agent.Busy():
			return "agent working"
		case m.agentSaw:
			return m.styles.ok.Render("agent done")
		default:
			return "agent ready"
		}
	}

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
