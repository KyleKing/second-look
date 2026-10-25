package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
)

// verdict is the ruling on a finding, and it is the state of the staged reply
// to the finding's thread: a thread with no reply, or a draft reply, has none.
type verdict int

const (
	vNone verdict = iota
	vReady
	vTodo
	vSkip
	vResolve
)

const replyPrefix = "reply-"

func replyID(commentID int64) string { return fmt.Sprintf("%s%d", replyPrefix, commentID) }

func (v verdict) word() string {
	switch v {
	case vReady:
		return artifact.StatusReady
	case vTodo:
		return artifact.StatusTodo
	case vSkip:
		return artifact.StatusSkip
	case vResolve:
		return "won't fix"
	case vNone:
	}

	return "no verdict"
}

func verdictOf(c *artifact.Comment) verdict {
	switch {
	case c.Status == artifact.StatusSkip:
		return vSkip
	case c.Status == artifact.StatusTodo:
		return vTodo
	case c.Status == artifact.StatusReady && c.Resolve:
		return vResolve
	case c.Status == artifact.StatusReady:
		return vReady
	}

	return vNone
}

// draftID names one request for reply text. It carries the verdict asked for,
// so an answer that lands after the verdict changed is recognized as stale.
func draftID(c *artifact.Comment) string { return c.ID + ":" + verdictOf(c).word() }

// replyIndex is the staged comment answering thread t, or -1.
func (m *Model) replyIndex(t int) int {
	to := m.threads[t].ReplyTo()
	if to == 0 {
		return -1
	}

	return slices.IndexFunc(m.review.Comments, func(c artifact.Comment) bool { return c.InReplyTo == to })
}

// verdictOfThread is the ruling on thread t.
func (m *Model) verdictOfThread(t int) verdict {
	if i := m.replyIndex(t); i >= 0 {
		return verdictOf(&m.review.Comments[i])
	}

	return vNone
}

// toJudge counts the open threads nobody has ruled on. A thread the viewer
// opened is theirs to defend rather than to judge.
func (m *Model) toJudge() int {
	return m.toJudgeIn(func(string) bool { return true })
}

// toJudgeIn is toJudge restricted to the threads on files keep accepts.
func (m *Model) toJudgeIn(keep func(path string) bool) int {
	return m.toJudgeWhere(func(t *threads.Thread) bool { return keep(t.Path) })
}

// toJudgeWhere is toJudge restricted to the threads keep accepts.
func (m *Model) toJudgeWhere(keep func(*threads.Thread) bool) int {
	n := 0

	for i := range m.threads {
		if keep(&m.threads[i]) && m.isFinding(&m.threads[i]) && m.verdictOfThread(i) == vNone {
			n++
		}
	}

	return n
}

func (m *Model) verdictKey(msg tea.KeyPressMsg) (verdict, bool) {
	switch {
	case key.Matches(msg, m.keys.Ready):
		return vReady, true
	case key.Matches(msg, m.keys.Todo):
		return vTodo, true
	case key.Matches(msg, m.keys.Skip):
		return vSkip, true
	case key.Matches(msg, m.keys.Resolve):
		return vResolve, true
	}

	return vNone, false
}

// threadHere is the thread whose card, or whose staged reply's card, the cursor
// is on, and -1 anywhere else.
func (m *Model) threadHere() int {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return -1
	}

	r := m.screen.rows[m.cursor]
	if r.kind == rowThread {
		return r.thread
	}

	if r.comment < 0 || r.comment >= len(m.review.Comments) || m.review.Comments[r.comment].InReplyTo == 0 {
		return -1
	}

	to := m.review.Comments[r.comment].InReplyTo

	return slices.IndexFunc(m.threads, func(t threads.Thread) bool { return t.ReplyTo() == to })
}

// judgeKey gives a verdict on the thread under the cursor, and reports whether
// the key was one.
func (m *Model) judgeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	v, ok := m.verdictKey(msg)
	t := m.threadHere()

	switch {
	case !ok:
		return nil, false
	case t < 0 && v == vResolve:
		m.say("R resolves a thread, and the cursor is not on one", false)

		return nil, true
	case t < 0:
		return nil, false
	}

	if m.settled() {
		m.say(m.closedWord(), false)

		return nil, true
	}

	return m.judge(t, v), true
}

// judge makes v the state of the reply to thread t, staging an empty one where
// the thread has none, and asks an agent for text when there is none to keep.
func (m *Model) judge(t int, v verdict) tea.Cmd {
	th := &m.threads[t]

	switch {
	case th.Resolved:
		m.say("this thread is already resolved on GitHub", false)

		return nil
	case v == vResolve && th.ID == "":
		m.say("this thread's id was never cached; second-look get reads it", true)

		return nil
	}

	i := m.replyIndex(t)
	if i < 0 {
		m.review.Upsert(artifact.Comment{
			ID: replyID(th.ReplyTo()), Path: th.Path, Side: th.Side, Line: th.Line,
			InReplyTo: th.ReplyTo(), ThreadID: th.ID, Severity: question,
		})
		i = len(m.review.Comments) - 1
	}

	c := &m.review.Comments[i]
	c.ThreadID = th.ID
	applyVerdict(c, v)

	var cmd tea.Cmd
	if c.Body == "" && v != vSkip {
		delete(m.failedAsks, draftID(c))
		cmd = m.draft(c, th, v)
	}

	m.save(fmt.Sprintf("%s is %s", c.ID, v.word()))

	return cmd
}

func applyVerdict(c *artifact.Comment, v verdict) {
	c.Resolve = false

	switch v {
	case vReady:
		setState(c, artifact.StatusReady)
	case vTodo:
		setState(c, artifact.StatusTodo)
	case vSkip:
		setState(c, artifact.StatusSkip)
	case vResolve:
		setState(c, artifact.StatusReady)
		c.Resolve = true
	case vNone:
	}
}

func (m *Model) draft(c *artifact.Comment, t *threads.Thread, v verdict) tea.Cmd {
	id, prompt := draftID(c), m.draftPrompt(t, v)

	switch {
	case m.agentFree():
		m.inFlight[id] = m.agent.Name()

		return askSession(m, m.agent, id, prompt)
	case m.asker != nil:
		return m.askOneShot(id, prompt)
	}

	return nil
}

// applyDrafted lands reply text in the card it was asked for. Text that came
// back after the verdict changed, or after the reviewer typed their own, is
// dropped.
func (m *Model) applyDrafted(msg askAnsweredMsg, who string) {
	i := slices.IndexFunc(m.review.Comments, func(c artifact.Comment) bool {
		return strings.HasPrefix(msg.id, c.ID+":")
	})
	text := strings.TrimSpace(msg.text)

	switch {
	case i < 0 || draftID(&m.review.Comments[i]) != msg.id:
		m.rebuild()
	case msg.err != nil:
		m.failedAsks[msg.id] = msg.err.Error()
		m.rebuild()
		m.say("drafting: "+msg.err.Error(), true)
	case text == "":
		m.failedAsks[msg.id] = "the agent said nothing"
		m.rebuild()
		m.say("the agent said nothing", false)
	case m.review.Comments[i].Body != "":
		m.rebuild()
	default:
		c := &m.review.Comments[i]
		c.Body, c.Origin = text, who
		m.save("drafted " + c.ID + "; e edits it")
	}
}

func (m *Model) draftPrompt(t *threads.Thread, v verdict) string {
	var said strings.Builder

	for _, n := range t.Notes {
		fmt.Fprintf(&said, "@%s:\n%s\n\n", n.Author, n.Body)
	}

	line, _ := m.diff.Anchor(t.Path, t.Side, t.Line)

	return fmt.Sprintf(`A reviewer is answering a review comment on a pull request. Write the reply
they would post, in the first person and plain text, in at most three sentences.
%s Answer with the reply alone.

The thread on %s:

%s
The line it is about:

%s
`, draftBrief[v], fmt.Sprintf("%s:%d", t.Path, t.Line), said.String(), line)
}

var draftBrief = map[verdict]string{
	vReady:   "They want to discuss it: ask the one question that settles it.",
	vResolve: `They will not act on it: open with "won't fix because" and give the reason.`,
	vTodo:    `They will fix it: open with "will fix:" and say what changes.`,
}

// isMine is a thread the viewer opened. With no viewer cached nobody's is.
func (m *Model) isMine(t *threads.Thread) bool {
	return m.about.Viewer != "" && len(t.Notes) > 0 && t.Notes[0].Author == m.about.Viewer
}

func (m *Model) isFinding(t *threads.Thread) bool {
	return !t.Resolved && len(t.Notes) > 0 && !m.isMine(t)
}

func (m *Model) judgeWord() string {
	n := m.toJudge()
	if n == 0 {
		return ""
	}

	return m.styles.priority.Render(agentGlyph + " " + judgeText(n))
}

// verdictWords says how each open finding stands, for the rule a folded thread
// is drawn as.
func (m *Model) verdictWords() map[int]string {
	out := map[int]string{}

	for i := range m.threads {
		if !m.isFinding(&m.threads[i]) {
			continue
		}

		out[i] = vNone.word()

		if r := m.replyIndex(i); r >= 0 {
			out[i] = standing(&m.review.Comments[r])
		}
	}

	return out
}

func standing(c *artifact.Comment) string {
	v := verdictOf(c)
	if v != vNone {
		return v.word()
	}

	if agentOrigin(c.Origin) {
		return agentGlyph + " reply drafted · no verdict"
	}

	return "reply drafted · no verdict"
}

func (l layout) unwrittenWord(c *artifact.Comment) string {
	id := draftID(c)

	switch _, waiting := l.inFlight[id]; {
	case waiting:
		return "drafting…"
	case l.failedAsks[id] != "":
		return l.failedAsks[id] + "; [e]dit to write one"
	}

	return "no reply text, [e]dit to write one"
}
