package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kyleking/aragonite/tui/vimtext"

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
	field *vimtext.Field
	title string
	// msg is where the text goes when it is saved, filled in by the same
	// handler an $EDITOR round trip answers through.
	msg editedMsg
	// key names this buffer on disk and start is what was in the field when the
	// editor opened, which is what ctrl+g puts back.
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

// editorPattern names the file the footer's ctrl+e promises. The screen runs
// $EDITOR in its own pane rather than through the field, so the field is given
// one only to name the key in its footer.
const editorPattern = "second-look-*.md"

// extraShare is the part of the footer the screen's own keys may take, one in
// this many cells, so the field's mode and its save key are never pushed off.
const extraShare = 2

// beginEdit opens the editor over the block the cursor is in, on whatever an
// earlier session left unfinished there if there is one.
func (m *Model) beginEdit(title, start string, msg editedMsg) tea.Cmd {
	m.editing = &editor{title: title, msg: msg, key: m.draftKey(msg), start: start}
	m.editing.field = m.newField(start,
		vimtext.WithHeight(editorMin, editorMax),
		vimtext.WithCompleter(m.completeWord),
		vimtext.WithEditor(editorPattern))
	m.say("", false)

	if msg.index == reviewBody {
		m.rebuild()
	}

	if body, at, ok := m.recall(); ok && body != start {
		m.editing.field.Reset(body)
		m.editing.restored = true
		m.say("unfinished from "+humanize.Ago(at, time.Now())+" ago, "+putBackKey+" puts back what is saved", false)
	}

	m.toHead()

	return nil
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

// The editor is a vim-modal field, which takes every key but a few: ctrl+e
// hands the buffer to $EDITOR rather than starting over in it, ctrl+t swaps in
// what arrived on disk, ctrl+g puts back what a restore replaced, and ctrl+c
// still quits. Enter saves in every mode, as it submits every prompt in the
// family (ctrl+j and alt+enter are the new line; ctrl+s, the save key until
// 0.61, still saves without saying so), and esc backs out a level at a time:
// insert to normal, and normal out of the editor with what was typed kept.
func (m *Model) typeBody(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case ctrlCKey:
		m.editing = nil
		m.rebuild()

		return m.handleKey(msg)
	case putBackKey:
		m.putBack()

		return m, nil
	case ctrlR:
		if m.editing.restored && m.editing.field.Mode() == vimtext.Insert {
			m.say("ctrl+r is redo here now; "+putBackKey+" drops the restored edit", false)

			return m, nil
		}
	case "ctrl+t":
		m.swapTheirs()

		return m, nil
	case "ctrl+s":
		m.saveEdit(m.editing.field.Value())

		return m, nil
	case "ctrl+e":
		done := m.editing
		m.editing = nil
		cmd := m.open(done.field.Value(), done.msg)

		return m, cmd
	}

	return m.feedEdit(msg)
}

// feedEdit hands a key or a paste to the field, and saves or leaves when it
// says the edit is over.
func (m *Model) feedEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	was := m.editing.field.Value()

	cmd, end := press(m.editing.field, msg)
	switch {
	case end.saved:
		m.saveEdit(end.text)

		return m, nil
	case end.left:
		m.leaveEdit(end.text)

		return m, nil
	}

	if m.editing.field.Value() != was {
		m.keep()

		if k, ok := msg.(tea.KeyPressMsg); ok && k.Text == "@" {
			cmd = tea.Batch(cmd, m.askRoster())
		}
	}

	return m, cmd
}

// saveEdit closes the editor on what it holds and sends that where it goes.
func (m *Model) saveEdit(text string) {
	done := m.editing
	m.editing = nil
	out := done.msg
	out.body = strings.TrimRight(text, "\n")
	m.applyEdit(out)
}

// leaveEdit closes the editor without saving. What was typed is kept on disk for
// the key that reopens it, unless it is what the field already said.
func (m *Model) leaveEdit(text string) {
	done := m.editing
	m.editing = nil
	m.rebuild()

	if text == done.start {
		m.drop(done.key)
		m.say("nothing changed", false)

		return
	}

	m.say("kept where you left it; "+reopens(done.msg)+" brings it back", false)
}

// swapTheirs puts the version an agent wrote into the buffer, and puts what was
// typed back on a second press. Neither side is thrown away, which is the whole
// point of holding both: the collision is a decision, not a merge.
func (m *Model) swapTheirs() {
	if m.editing.theirs == "" {
		m.say("nothing arrived under this one", false)

		return
	}

	mine := m.editing.field.Value()
	m.editing.field.SetValue(m.editing.theirs)
	m.editing.theirs = mine
	m.keep()
	m.say("swapped; ctrl+t again puts the other one back", false)
}

// reopens names the key that offers a kept buffer back, since a comment that
// does not exist yet is reached by i rather than by e, and a marker by F.
func reopens(msg editedMsg) string {
	if msg.fresh != nil && msg.fresh.marker {
		return "F"
	}

	if msg.fresh != nil {
		return "i"
	}

	return "e"
}

// putBack answers ctrl+g: it drops what was restored and starts from what the
// field says now, which is the other half of the decision a restore is.
func (m *Model) putBack() {
	if !m.editing.restored {
		m.say("nothing was restored here", false)

		return
	}

	m.editing.field.SetValue(m.editing.start)
	m.editing.restored = false

	m.drop(m.editing.key)
	m.say("dropped the unfinished edit", false)
}

// keep writes the buffer through on every keystroke, so a terminal that dies
// mid-sentence loses nothing. A buffer back at what the field already says is
// removed rather than kept, since there would be nothing to offer.
func (m *Model) keep() {
	if m.store == "" || m.editing.told {
		return
	}

	if m.editing.field.Value() == m.editing.start {
		m.drop(m.editing.key)

		return
	}

	if err := artifact.SaveDraft(m.store, m.editing.key, m.editing.field.Value()); err != nil {
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
// itself, and a footer with the field's mode and the keys it takes, the
// screen's own after the field's. Width is the body's, which leaves the
// scrollbar its column.
func (m *Model) editorLines(width int) []string {
	gutter := strings.Repeat(" ", m.screen.numWidth+indent)
	frame := m.styles.warn
	room := max(1, width-textWidth(gutter+"┃ "))

	// The corners reach right into a rule, so neither is an arm joining
	// nothing.
	out := []string{frame.Render(cut(gutter+"┏━ "+m.editing.title, width))}

	for _, line := range strings.Split(m.editing.field.View(room), "\n") {
		out = append(out, frame.Render(gutter+"┃ ")+cut(line, room))
	}

	return append(out, frame.Render(gutter+"┗━ ")+m.editorFooter(max(1, room-1)))
}

// editorFooter is the field's footer with what this screen adds while it
// applies, which goes first when there is not room for both: a key that is
// only offered now is the one nobody can learn from the legend.
func (m *Model) editorFooter(width int) string {
	var extra []string
	if m.editing.restored {
		extra = append(extra, putBackKey+" drop restored")
	}

	if m.editing.theirs != "" {
		extra = append(extra, "ctrl+t disk version")
	}

	if len(extra) == 0 {
		return m.editing.field.Footer(width)
	}

	tail := " · " + strings.Join(extra, " · ")
	if most := width / extraShare; textWidth(tail) >= most {
		tail = ansi.Truncate(tail, most, "…")
	}

	return m.editing.field.Footer(width-textWidth(tail)) + m.styles.warn.Render(tail)
}

// answering reports an edit that adds to a thread rather than rewriting a
// comment. What is being answered has to stay on screen: a reply written
// against a bot's finding is written from the finding.
func (m *Model) answering() bool {
	return m.editing != nil && m.editing.msg.fresh == nil && m.editing.msg.replyTo >= 0
}

// answeredLines is the thread being answered with the editor under it, in the
// room the frame has left. A thread too tall for that keeps its last turns,
// which are the ones the reply is to, rather than losing the box off the
// bottom of the frame.
func (m *Model) answeredLines(from, room, width int) []string {
	box := m.editorLines(width)
	if room <= len(box) {
		return box
	}

	end := m.spanEnd(from)
	if fits := room - len(box); end-from+1 > fits {
		from = end - fits + 1
	}

	out := make([]string, 0, room)
	for i := from; i <= end; i++ {
		out = append(out, m.renderRow(i, width)...)
	}

	return append(out, box...)
}
