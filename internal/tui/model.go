package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

// Minimum usable frame. Below it the screen is a resize message rather than a
// diff wrapped into illegibility.
const (
	minWidth  = 80
	minHeight = 10
	// StartHeight is what the screen assumes until the terminal says otherwise.
	startHeight = 24
)

// Submitter posts the prepared review and reports what happened. It is a
// parameter so the screen does not depend on how a review reaches GitHub, and
// so a test can drive the submit path without a network.
type Submitter func(ctx context.Context, r *artifact.Review) (string, error)

// Model is the review screen.
type Model struct {
	ctx    context.Context //nolint:containedctx // it bounds the editor subprocess and the post
	review *artifact.Review
	diff   *diff.Diff
	path   string
	submit Submitter

	screen screen
	cursor int
	offset int
	width  int
	height int

	keys   keyMap
	styles styles

	status string
	failed bool
	posted bool
	help   bool
}

// New builds the review screen for a prepared review and the diff it was
// staged against.
func New(ctx context.Context, r *artifact.Review, d *diff.Diff, path string, submit Submitter) *Model {
	return &Model{
		ctx: ctx, review: r, diff: d, path: path, submit: submit,
		keys: defaultKeyMap(), styles: newStyles(),
		width: minWidth, height: startHeight,
	}
}

// Init lays out the first frame at the assumed size, which the terminal
// corrects with a resize before anything is drawn.
func (m *Model) Init() tea.Cmd {
	m.rebuild()

	return nil
}

type editedMsg struct {
	index int
	body  string
	err   error
}

type submittedMsg struct {
	summary string
	err     error
}

// Update routes one message. Movement is separated from the keys that change
// the review, so what can alter a comment stays a short list.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()

		return m, nil
	case editedMsg:
		m.applyEdit(msg)

		return m, nil
	case submittedMsg:
		m.applySubmit(msg)

		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		if m.help {
			m.help = false

			return m, nil
		}

		return m, tea.Quit
	}

	if key.Matches(msg, m.keys.Help) {
		m.help = !m.help

		return m, nil
	}

	if m.moved(msg) {
		return m, nil
	}

	return m.act(msg)
}

// moved handles every key that only changes where the cursor is, so the action
// keys below stay a flat list of things that change the review.
func (m *Model) moved(msg tea.KeyPressMsg) bool {
	const halfPage = 2

	half := m.viewHeight() / halfPage

	switch {
	case key.Matches(msg, m.keys.Down):
		m.moveBy(1)
	case key.Matches(msg, m.keys.Up):
		m.moveBy(-1)
	case key.Matches(msg, m.keys.HalfDown):
		m.moveBy(half)
	case key.Matches(msg, m.keys.HalfUp):
		m.moveBy(-half)
	case key.Matches(msg, m.keys.Top):
		m.cursor = 0
	case key.Matches(msg, m.keys.Bottom):
		m.cursor = len(m.screen.rows) - 1
	case key.Matches(msg, m.keys.NextHunk):
		m.jump(1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, m.keys.PrevHunk):
		m.jump(-1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, m.keys.NextFile):
		m.jump(1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, m.keys.PrevFile):
		m.jump(-1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, m.keys.NextNote):
		m.jump(1, func(r row) bool { return r.head })
	case key.Matches(msg, m.keys.PrevNote):
		m.jump(-1, func(r row) bool { return r.head })
	default:
		return false
	}

	m.follow()

	return true
}

func (m *Model) act(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Ready):
		m.setStatus(artifact.StatusReady)
	case key.Matches(msg, m.keys.Draft):
		m.setStatus(artifact.StatusDraft)
	case key.Matches(msg, m.keys.Skip):
		m.setStatus(artifact.StatusSkip)
	case key.Matches(msg, m.keys.Edit):
		cmd := m.edit()

		return m, cmd
	case key.Matches(msg, m.keys.Submit):
		cmd := m.submitCmd()

		return m, cmd
	}

	return m, nil
}

// current is the comment the cursor is inside, or -1 when it is on code.
func (m *Model) current() int {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return -1
	}

	return m.screen.rows[m.cursor].comment
}

func (m *Model) setStatus(status string) {
	i := m.current()
	if i < 0 {
		m.say("no comment here", false)

		return
	}

	c := &m.review.Comments[i]
	c.Status = status

	// Validation requires a reason for a skip, and refusing the keystroke over
	// a sentence nobody has written yet would be worse than a plain default.
	switch {
	case status != artifact.StatusSkip:
		c.SkipReason = ""
	case c.SkipReason == "":
		c.SkipReason = "declined during review"
	}

	m.save(fmt.Sprintf("%s is %s", c.ID, status))
}

func (m *Model) save(ok string) {
	if err := artifact.Save(m.path, m.review); err != nil {
		m.say(err.Error(), true)

		return
	}

	m.rebuild()
	m.say(ok, false)
}

func (m *Model) say(text string, failed bool) {
	m.status, m.failed = text, failed
}

// edit opens the comment body in $EDITOR, which owns the terminal until it
// exits, following the same rule as running the code under review.
func (m *Model) edit() tea.Cmd {
	i := m.current()
	if i < 0 {
		m.say("no comment here", false)

		return nil
	}

	file, err := os.CreateTemp("", "second-look-*.md")
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	name := file.Name()
	if _, err := file.WriteString(m.review.Comments[i].Body); err != nil {
		m.say(err.Error(), true)

		return nil
	}

	if err := file.Close(); err != nil {
		m.say(err.Error(), true)

		return nil
	}

	return tea.ExecProcess(editorCmd(m.ctx, name), func(err error) tea.Msg {
		//nolint:errcheck // a temp file that outlives the edit is not worth an error path
		defer os.Remove(name)

		if err != nil {
			return editedMsg{index: i, err: err}
		}

		body, err := os.ReadFile(name) //nolint:gosec // our own temp file
		if err != nil {
			return editedMsg{index: i, err: err}
		}

		return editedMsg{index: i, body: strings.TrimRight(string(body), "\n")}
	})
}

func editorCmd(ctx context.Context, path string) *exec.Cmd {
	fields := strings.Fields(os.Getenv("EDITOR"))
	if len(fields) == 0 {
		fields = []string{"vi"}
	}

	//nolint:gosec // the command is the user's own EDITOR and the path is our temp file
	return exec.CommandContext(ctx, fields[0], append(fields[1:], path)...)
}

func (m *Model) applyEdit(msg editedMsg) {
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	if msg.body == "" {
		m.say("empty body, nothing changed", false)

		return
	}

	m.review.Comments[msg.index].Body = msg.body
	m.save("edited " + m.review.Comments[msg.index].ID)
}

func (m *Model) submitCmd() tea.Cmd {
	if m.posted {
		m.say("already posted", false)

		return nil
	}

	m.say("posting…", false)
	ctx, review := m.ctx, m.review

	return func() tea.Msg {
		summary, err := m.submit(ctx, review)

		return submittedMsg{summary: summary, err: err}
	}
}

func (m *Model) applySubmit(msg submittedMsg) {
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	m.posted = true
	m.say(msg.summary+" — press q", false)
}

func (m *Model) rebuild() {
	m.screen = build(m.review, m.diff, m.width)
	m.cursor = clamp(m.cursor, 0, len(m.screen.rows)-1)
	m.follow()
}

func (m *Model) moveBy(n int) {
	m.cursor = clamp(m.cursor+n, 0, len(m.screen.rows)-1)
}

func (m *Model) jump(step int, want func(row) bool) {
	for i := m.cursor + step; i >= 0 && i < len(m.screen.rows); i += step {
		if want(m.screen.rows[i]) {
			m.cursor = i

			return
		}
	}
}

// follow keeps the cursor on screen, scrolling by the smallest amount that
// brings it back into the frame. Landing on a comment reveals the rest of it
// where the frame has room, since a comment's first line is its severity and
// the sentence under it is the part worth reading.
func (m *Model) follow() {
	h := m.viewHeight()

	m.offset = min(m.offset, m.cursor)
	m.offset = max(m.offset, min(m.blockEnd(), m.cursor+h-1)-h+1)
	m.offset = clamp(m.offset, 0, max(0, len(m.screen.rows)-h))
}

// blockEnd is the last row of the comment the cursor is in, or the cursor.
func (m *Model) blockEnd() int {
	c := m.current()
	if c < 0 {
		return m.cursor
	}

	end := m.cursor
	for end+1 < len(m.screen.rows) && m.screen.rows[end+1].comment == c {
		end++
	}

	return end
}

// viewHeight is the frame minus the title and footer lines.
func (m *Model) viewHeight() int {
	const chrome = 2

	return max(1, m.height-chrome)
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, max(lo, hi)))
}
