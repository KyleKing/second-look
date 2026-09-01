package tui

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
)

// severities are what the second key of the a chord can say a new comment is.
// A comment written here is ranked the way an agent's is, because the ranking
// is what orders which findings get read first.
//
// A question is the one severity not keyed by its own first letter, because
// every chord takes q as its cancel and a second key that quietly means two
// things is worse than an odd one.
func severities() [][2]string {
	return [][2]string{
		{"b", "blocker"},
		{"m", "major"},
		{"n", "minor"},
		{"t", "nit"},
		{"?", question},
	}
}

// question is the severity of a comment that asks rather than tells, which is
// what a reply staged from a thread is and what the a chord's q writes.
const question = "question"

// staging is a comment being written for the first time: where it lands and how
// it is ranked, both settled before the editor opens so the buffer that comes
// back needs nothing asked of it.
type staging struct {
	path     string
	side     string
	severity string
	line     int
}

// mine is the id a comment written on this screen carries, so what a person
// wrote is told apart from what an agent staged at a glance.
const mine = "mine-"

// askWrite opens the a chord. A review comment anchors to a line of the diff
// and nowhere else, so a row that is not one is refused here rather than after
// the comment has been typed.
func (m *Model) askWrite() {
	if !m.onCode() {
		m.say("a writes a comment on a line of the diff, and this row is not one", false)

		return
	}

	m.pending = 'a'
	m.say(m.chord("a", severities()), false)
}

// writeAs answers the second key of the a chord and opens the editor on the
// line the cursor is standing on.
func (m *Model) writeAs(msg tea.KeyPressMsg) tea.Cmd {
	var severity string

	for _, s := range severities() {
		if s[0] == msg.String() {
			severity = s[1]
		}
	}

	if severity == "" {
		m.say("no severity for "+msg.String()+"; "+hintLine(styles{}, severities()), false)

		return nil
	}

	r := m.screen.rows[m.cursor]
	a := anchorOf(r.path, r.line)

	fresh := &staging{path: a.path, side: a.side, line: a.line, severity: severity}
	title := fmt.Sprintf("a %s comment on %s:%d", severity, a.path, a.line)

	return m.beginEdit(title, "", editedMsg{index: noComment, replyTo: -1, fresh: fresh})
}

// draftKey names the buffer on disk for whatever is being written, so a second
// edit of the same thing is offered what the first left. A reply is keyed by
// the comment it answers rather than by the thread's place in the list, which
// changes as threads are resolved.
func (m *Model) draftKey(msg editedMsg) string {
	switch {
	case msg.fresh != nil:
		f := msg.fresh

		return fmt.Sprintf("new-%s-%s-%d", f.path, f.side, f.line)
	case msg.replyTo >= 0 && msg.replyTo < len(m.threads):
		return fmt.Sprintf("reply-%d", m.threads[msg.replyTo].ReplyTo())
	case msg.index == reviewBody:
		return "review-body"
	case msg.index == reviewNote:
		return "review-note"
	case msg.index < 0 || msg.index >= len(m.review.Comments):
		return "unplaced"
	case msg.field == fieldNote:
		return "note-" + m.review.Comments[msg.index].ID
	}

	return "body-" + m.review.Comments[msg.index].ID
}

// stageNew puts a comment written here into the prepared review, quoting the
// line it anchors to the same way staging an agent's comment does. It is ready
// rather than draft for the reason a reply is: a draft is a comment nobody has
// ruled on, and this one was typed by the person the screen belongs to.
func (m *Model) stageNew(msg editedMsg) {
	f := msg.fresh

	text, ok := m.diff.Anchor(f.path, f.side, f.line)
	if !ok {
		m.say(f.path+" line "+strconv.Itoa(f.line)+" is not in this diff", true)

		return
	}

	c := artifact.Comment{
		ID: m.freeID(), Path: f.path, Side: f.side, Line: f.line, Body: msg.body,
		Anchor: text, Severity: f.severity, Status: artifact.StatusReady,
	}

	m.review.Upsert(c)
	m.save("staged " + c.ID + ", ready to post")
	m.focus(len(m.review.Comments) - 1)
}

// freeID is the lowest mine-N nothing is using. An id is what a hand edit and
// an agent update both address a comment by, so a second one cannot reuse the
// first's name.
func (m *Model) freeID() string {
	taken := make(map[string]bool, len(m.review.Comments))
	for i := range m.review.Comments {
		taken[m.review.Comments[i].ID] = true
	}

	for n := 1; ; n++ {
		id := mine + strconv.Itoa(n)
		if !taken[id] {
			return id
		}
	}
}
