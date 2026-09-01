package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
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
}

// beginEdit opens the editor over the block the cursor is in.
func (m *Model) beginEdit(title, start string, msg editedMsg) tea.Cmd {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetVirtualCursor(true)
	area.SetWidth(max(1, m.width-m.screen.numWidth-rail))
	area.SetValue(start)
	area.MoveToEnd()

	m.editing = &editor{area: area, title: title, msg: msg}
	m.fit()
	m.toHead()
	m.say("", false)

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
		m.editing = nil
		m.say("nothing changed", false)

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

	return m, cmd
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

	const keys = "┗ ctrl+s save · esc cancel · ctrl+e $EDITOR"

	return append(out, frame.Render(cut(gutter+keys, m.width)))
}
