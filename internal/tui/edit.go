package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/humanize"
)

// How tall the in-place editor is allowed to grow. It starts at the height of
// what it holds so a one-line comment costs one line, and stops well short of
// the frame so the code the comment is about is never entirely pushed off.
const (
	editorMin = 1
	editorMax = 12
)

// editor is prose being written where it will end up, in place of the block it
// replaces. Everything longer than a few sentences still belongs in $EDITOR,
// which ctrl+e hands the buffer to without losing what is typed so far.
type editor struct {
	area  textarea.Model
	title string
	// msg is where the text goes when it is saved, filled in by the same
	// handler an $EDITOR round trip answers through.
	msg editedMsg
	// key names this buffer on disk and start is what was in the field when the
	// editor opened, which is what ctrl+r puts back.
	key   string
	start string
	// restored marks a buffer that came back from an earlier session rather
	// than from the review, so the editor can say so and offer to drop it.
	restored bool
	// told marks a failure to keep the buffer as already reported, since it
	// would otherwise be reported on every keystroke.
	told bool
	// theirs is the body this comment gained on disk while it was being typed
	// in. It is held rather than applied: ctrl+t swaps it into the buffer, and
	// what was typed goes back on a second press.
	theirs string
}

// beginEdit opens the editor over the block the cursor is in, on whatever an
// earlier session left unfinished there if there is one.
func (m *Model) beginEdit(title, start string, msg editedMsg) tea.Cmd {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetVirtualCursor(true)
	area.SetWidth(max(1, m.width-m.screen.numWidth-rail))
	area.SetValue(start)
	area.MoveToEnd()

	m.editing = &editor{area: area, title: title, msg: msg, key: m.draftKey(msg), start: start}
	m.say("", false)

	if body, at, ok := m.recall(); ok && body != start {
		area.SetValue(body)
		area.MoveToEnd()

		m.editing.area, m.editing.restored = area, true
		m.say("unfinished from "+humanize.Ago(at, time.Now())+" ago, ctrl+r puts back what is saved", false)
	}

	m.fit()
	m.toHead()

	// Focus takes a pointer receiver, so it has to be called on the editor the
	// screen kept rather than on the copy that was put into it.
	return m.editing.area.Focus()
}

// fit grows the box with what is in it, so an answer of one line is not framed
// by a screenful of empty rows. The count is of wrapped lines rather than the
// textarea's own logical ones, and its MaxHeight is left unset, because that
// field stops the typing rather than the growing.
func (m *Model) fit() {
	shown := len(wrap(m.editing.area.Value(), m.editing.area.Width()))
	m.editing.area.SetHeight(min(max(shown, editorMin), editorMax))
}

// toHead puts the cursor on the row the editor replaces, so scrolling keeps the
// box on screen the way it keeps a comment on screen.
func (m *Model) toHead() {
	for i := m.cursor; i >= 0; i-- {
		if m.editingHere(i) || m.editingUnder(i) {
			m.cursor = i
			m.reveal()

			return
		}
	}
}

// The editor takes every key but three: ctrl+s saves, esc abandons what was
// typed, and ctrl+e hands the buffer to $EDITOR rather than starting over in it.
func (m *Model) typeBody(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		done := m.editing
		m.editing = nil

		if done.area.Value() == done.start {
			m.drop(done.key)
			m.say("nothing changed", false)

			return m, nil
		}

		m.say("kept where you left it; "+reopens(done.msg)+" brings it back", false)

		return m, nil
	case refreshKey:
		m.putBack()

		return m, nil
	case "ctrl+t":
		m.swapTheirs()

		return m, nil
	case "ctrl+n":
		m.suggestWord()

		return m, nil
	case "ctrl+s":
		done := m.editing
		m.editing = nil
		out := done.msg
		out.body = strings.TrimRight(done.area.Value(), "\n")
		m.applyEdit(out)

		return m, nil
	case "ctrl+e":
		done := m.editing
		m.editing = nil
		cmd := m.open(done.area.Value(), done.msg)

		return m, cmd
	}

	area, cmd := m.editing.area.Update(msg)
	m.editing.area = area
	m.fit()
	m.keep()

	return m, cmd
}

// swapTheirs puts the version an agent wrote into the buffer, and puts what was
// typed back on a second press. Neither side is thrown away, which is the whole
// point of holding both: the collision is a decision, not a merge.
func (m *Model) swapTheirs() {
	if m.editing.theirs == "" {
		m.say("nothing arrived under this one", false)

		return
	}

	mine := m.editing.area.Value()
	m.editing.area.SetValue(m.editing.theirs)
	m.editing.area.MoveToEnd()
	m.editing.theirs = mine
	m.fit()
	m.say("swapped; ctrl+t again puts the other one back", false)
}

// reopens names the key that offers a kept buffer back, since a comment that
// does not exist yet is reached by a rather than by e.
func reopens(msg editedMsg) string {
	if msg.fresh != nil {
		return "a"
	}

	return "e"
}

// putBack answers ctrl+r: it drops what was restored and starts from what the
// field says now, which is the other half of the decision a restore is.
func (m *Model) putBack() {
	if !m.editing.restored {
		m.say("nothing was restored here", false)

		return
	}

	m.editing.area.SetValue(m.editing.start)
	m.editing.area.MoveToEnd()
	m.editing.restored = false

	m.drop(m.editing.key)
	m.fit()
	m.say("dropped the unfinished edit", false)
}

// keep writes the buffer through on every keystroke, so a terminal that dies
// mid-sentence loses nothing. A buffer back at what the field already says is
// removed rather than kept, since there would be nothing to offer.
func (m *Model) keep() {
	if m.store == "" || m.editing.told {
		return
	}

	if m.editing.area.Value() == m.editing.start {
		m.drop(m.editing.key)

		return
	}

	if err := artifact.SaveDraft(m.store, m.editing.key, m.editing.area.Value()); err != nil {
		m.editing.told = true
		m.say(err.Error(), true)
	}
}

// recall is what was left unfinished where the editor has just opened.
func (m *Model) recall() (string, time.Time, bool) {
	if m.store == "" {
		return "", time.Time{}, false
	}

	return artifact.LoadDraft(m.store, m.editing.key)
}

func (m *Model) drop(key string) {
	if m.store == "" {
		return
	}

	if err := artifact.DropDraft(m.store, key); err != nil {
		m.say(err.Error(), true)
	}
}

// editingHere reports whether the editor stands in for the block that starts at
// row i. A comment written from nothing replaces no block, so it is drawn under
// the line it will anchor to instead.
func (m *Model) editingHere(i int) bool {
	if m.editing == nil || m.editing.msg.fresh != nil || i < 0 || i >= len(m.screen.rows) {
		return false
	}

	r := m.screen.rows[i]
	if !r.head {
		return false
	}

	if m.editing.msg.replyTo >= 0 {
		return r.kind == rowThread && r.thread == m.editing.msg.replyTo
	}

	return r.kind == rowComment && r.comment == m.editing.msg.index
}

// editingUnder reports whether the comment being written belongs under row i,
// which is the line of the diff it will anchor to.
func (m *Model) editingUnder(i int) bool {
	if m.editing == nil || m.editing.msg.fresh == nil || i < 0 || i >= len(m.screen.rows) {
		return false
	}

	r := m.screen.rows[i]
	if r.kind != rowCode {
		return false
	}

	f := m.editing.msg.fresh

	return anchorOf(r.path, r.line) == anchor{path: f.path, side: f.side, line: f.line}
}

// spanEnd is the last row of the block starting at from.
func (m *Model) spanEnd(from int) int {
	head := m.screen.rows[from]
	end := from

	for end+1 < len(m.screen.rows) {
		next := m.screen.rows[end+1]

		if head.kind == rowThread {
			if next.kind != rowThread || next.thread != head.thread {
				break
			}
		} else if next.comment != head.comment {
			break
		}

		end++
	}

	return end
}

// editorLines is the box: a heading naming what is being written, the text
// itself, and the keys that end the edit.
func (m *Model) editorLines() []string {
	gutter := strings.Repeat(" ", m.screen.numWidth+indent)
	frame := m.styles.warn

	out := []string{frame.Render(cut(gutter+"┏ "+m.editing.title, m.width))}

	for _, line := range strings.Split(m.editing.area.View(), "\n") {
		out = append(out, frame.Render(gutter+"┃ ")+cut(line, m.width))
	}

	keys := "┗ ctrl+s save · esc keeps it · ctrl+e $EDITOR · ctrl+n complete"
	if m.editing.restored {
		keys += " · ctrl+r drops what was restored"
	}

	if m.editing.theirs != "" {
		keys += " · ctrl+t swaps the version from disk"
	}

	return append(out, frame.Render(cut(gutter+keys, m.width)))
}
