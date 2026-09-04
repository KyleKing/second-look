package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/brief"
)

const (
	setDirPerm  = 0o750
	setFilePerm = 0o600
)

// dispatch answers T: every comment handed back to an agent is written out as
// one set, and the agent is started on it where one is configured.
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

	run := m.dispatcher

	return func() tea.Msg {
		out, err := run(context.Background(), path)

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
