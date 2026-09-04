package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

// suggest answers s: the line under the cursor is opened as the text it will be
// replaced with, and what comes back is staged as a GitHub suggestion.
//
// Writing one by hand means typing three fences and the line's own leading
// whitespace correctly, which is why nobody does it from a terminal. Opening it
// on the line's own text is the whole feature: an edit of what is there beats a
// paragraph describing the edit.
func (m *Model) suggest() tea.Cmd {
	if !m.onCode() {
		m.say("s suggests a replacement for a line of the diff, and this row is not one", false)

		return nil
	}

	r := m.screen.rows[m.cursor]
	if r.line.Kind == diff.KindRemove {
		m.say("a suggestion replaces a line of the file that results, and this one came out", false)

		return nil
	}

	start, line, _ := m.takeRange(r.line, r.path)
	from := firstOf(start, line)

	text, ok := m.rangeText(r.path, artifact.SideRight, from, line)
	if !ok {
		m.say("a suggestion replaces lines of the file that results, and this range crosses one that came out", true)

		return nil
	}

	fresh := &staging{path: r.path, side: artifact.SideRight, line: line, start: start, severity: question}

	title := "suggest a replacement for " + rangeWord(r.path, from, line)

	return m.beginEdit(title, text, editedMsg{
		index: noComment, replyTo: -1, fresh: fresh, suggests: true,
	})
}

// stageSuggestion wraps what was typed as a suggestion block and refuses what
// GitHub would refuse, here rather than at post time.
func (m *Model) stageSuggestion(msg editedMsg) {
	f := msg.fresh

	text, ok := m.diff.Anchor(f.path, f.side, f.line)
	if !ok {
		m.say(f.path+" line "+strconv.Itoa(f.line)+" is not in this diff", true)

		return
	}

	c := artifact.Comment{
		ID: m.freeID(), Path: f.path, Side: f.side, Line: f.line,
		StartLine: f.start, StartSide: startSide(f),
		Body: artifact.Suggest(msg.body), Anchor: text,
		Severity: f.severity, Status: artifact.StatusReady,
	}

	if err := artifact.CheckSuggestion(&c, m.diff); err != nil {
		m.say(err.Error(), true)

		return
	}

	m.review.Upsert(c)
	m.save("staged " + c.ID + " as a suggestion, ready to post")
	m.focus(len(m.review.Comments) - 1)
}
