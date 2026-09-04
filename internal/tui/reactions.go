package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/react"
	"github.com/kyleking/second-look/internal/threads"
)

// reactObjects are what the , chord accepts, shown while it waits so the second
// key never has to be remembered.
func reactObjects() [][2]string {
	all := react.Emojis()
	out := make([][2]string, 0, len(all))

	for _, e := range all {
		out = append(out, [2]string{e.Key, e.Glyph})
	}

	return out
}

// reactionLine is the emoji left on a comment, with the ones you left marked.
// A queue answered twice used to leave a second thumbs-up on the same finding,
// because nothing on screen said the first one was yours.
func reactionLine(n *threads.Note) string {
	if len(n.Reactions) == 0 {
		return ""
	}

	parts := make([]string, 0, len(n.Reactions))

	for _, r := range n.Reactions {
		word := react.Glyph(r.Content) + " " + strconv.Itoa(r.Count)
		if r.Mine {
			word += " ✓"
		}

		parts = append(parts, word)
	}

	return strings.Join(parts, "  ")
}

// reactTo leaves the emoji the second key names on the comment that opened the
// thread under the cursor, and takes it back where it is already yours.
//
// The first comment is the one reacted to for the same reason a resolve marks
// it: the point being acknowledged is the finding rather than the last word
// about it.
func (m *Model) reactTo(msg tea.KeyPressMsg) {
	e, ok := react.ByKey(msg.String())
	if !ok {
		m.say("no reaction for "+msg.String()+"; "+m.chord(",", reactObjects()), true)

		return
	}

	at := m.currentThread()
	if at < 0 || at >= len(m.threads) || len(m.threads[at].Notes) == 0 {
		m.say("reactions go on a conversation; ]t is the next one", true)

		return
	}

	if m.reactor == nil {
		m.say("this screen cannot react", true)

		return
	}

	note := &m.threads[at].Notes[0]
	mine := hasReacted(note, e.Content)

	if err := m.reactor(m.ctx, note.NodeID, e.Content, mine); err != nil {
		m.say(err.Error(), true)

		return
	}

	turn(note, e.Content, !mine)
	m.rebuild()

	if mine {
		m.say("took back "+e.Glyph, false)

		return
	}

	m.say("reacted "+e.Glyph, false)
}

func hasReacted(n *threads.Note, content string) bool {
	for i := range n.Reactions {
		if n.Reactions[i].Content == content {
			return n.Reactions[i].Mine
		}
	}

	return false
}

// turn records what the mutation just did, so the screen says so without
// reading the pull request again.
func turn(n *threads.Note, content string, mine bool) {
	for i := range n.Reactions {
		if n.Reactions[i].Content != content {
			continue
		}

		n.Reactions[i].Mine = mine

		if mine {
			n.Reactions[i].Count++

			return
		}

		n.Reactions[i].Count--
		if n.Reactions[i].Count <= 0 {
			n.Reactions = append(n.Reactions[:i], n.Reactions[i+1:]...)
		}

		return
	}

	if mine {
		n.Reactions = append(n.Reactions, threads.Reaction{Content: content, Count: 1, Mine: true})
	}
}
