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

// Scroll placement. A line-at-a-time move keeps scrollOff rows of context
// between the cursor and the frame's edge. A jump instead leaves only
// jumpMargin rows of what came before, because a heading is worth reading with
// the content under it.
const (
	scrollOff  = 3
	jumpMargin = 1
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

	status     string
	failed     bool
	posted     bool
	posting    bool
	confirming bool
	help       bool
	// failure is the last submit that did not post, cleared by one that does.
	// The screen leaves through it, so a run that failed to post says so on
	// stdout and in the exit code rather than only in a footer nobody kept.
	failure error
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

		// The frame after a post differs from the one before it on the footer
		// alone, and bubbletea v2.0.8 blanks the run of cells a redrawn line
		// shares with the line it replaces ("posting…" then "posted to …"
		// renders as "    ed to …"). Drop the repaint once that is fixed
		// upstream; until then this is the one line that must be readable.
		return m, tea.ClearScreen
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirming {
		return m.answer(msg)
	}

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
		return m.jump(1, isKind(rowHunk))
	case key.Matches(msg, m.keys.PrevHunk):
		return m.jump(-1, isKind(rowHunk))
	case key.Matches(msg, m.keys.NextFile):
		return m.jump(1, isKind(rowFile))
	case key.Matches(msg, m.keys.PrevFile):
		return m.jump(-1, isKind(rowFile))
	case key.Matches(msg, m.keys.NextNote):
		return m.jump(1, isHead)
	case key.Matches(msg, m.keys.PrevNote):
		return m.jump(-1, isHead)
	default:
		return false
	}

	m.follow()

	return true
}

func isKind(k rowKind) func(row) bool {
	return func(r row) bool { return r.kind == k }
}

func isHead(r row) bool { return r.head }

func (m *Model) act(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Posting removes the prepared review, and every action below writes it
	// back. One keystroke after a successful post would recreate the file that
	// was deleted to stop `second-look post` from publishing a second copy.
	if m.settled() && m.changes(msg) {
		m.say("already posted; GitHub has this review now", false)

		return m, nil
	}

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
		m.askSubmit()
	}

	return m, nil
}

// changes reports whether a key writes to the prepared review.
func (m *Model) changes(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Ready) ||
		key.Matches(msg, m.keys.Draft) ||
		key.Matches(msg, m.keys.Skip) ||
		key.Matches(msg, m.keys.Edit)
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

// askSubmit asks before it posts. Posting is the only thing the screen does
// that cannot be taken back, and S sits one shift away from the keys that mark
// a comment ready.
func (m *Model) askSubmit() {
	// posting is set from the moment the request is dispatched, not when it
	// answers, because the keys pressed while it is in flight arrive first and
	// a second confirmed S would publish the review twice.
	if m.posting {
		m.say("posting…", false)

		return
	}

	if m.posted {
		m.say("already posted", false)

		return
	}

	if m.review.Empty() {
		m.say(artifact.ErrNothingToPost.Error(), true)

		return
	}

	c := m.counts()
	if c.draft > 0 {
		m.focus(m.firstDraft())
		m.say(fmt.Sprintf("%d comment(s) still draft, r to post it or x to skip it", c.draft), true)

		return
	}

	m.confirming = true
	// The pull request is already named in the title bar, so the prompt spends
	// its width on what the keys do and stays readable in an 80-column frame.
	m.say(fmt.Sprintf("S again to post, any key cancels: %d comment(s) as %s", c.ready, m.event()), false)
}

// answer reads the reply to the submit prompt. Anything but a second S cancels
// and is swallowed, so no keystroke meant for the review posts it instead.
func (m *Model) answer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.confirming = false

	if !key.Matches(msg, m.keys.Submit) {
		m.say("canceled, nothing was posted", false)

		return m, nil
	}

	m.posting = true

	m.say("posting…", false)

	ctx, review := m.ctx, m.review

	return m, func() tea.Msg {
		summary, err := m.submit(ctx, review)

		return submittedMsg{summary: summary, err: err}
	}
}

func (m *Model) event() string {
	if m.review.Event == "" {
		return artifact.EventComment
	}

	return m.review.Event
}

func (m *Model) firstDraft() int {
	for i := range m.review.Comments {
		if m.review.Comments[i].Status == artifact.StatusDraft {
			return i
		}
	}

	return -1
}

// focus puts the cursor on a comment by index, so a refusal points at what has
// to change rather than only counting it.
func (m *Model) focus(index int) {
	for i, r := range m.screen.rows {
		if r.head && r.comment == index {
			m.cursor = i
			m.reveal()

			return
		}
	}
}

func (m *Model) settled() bool { return m.posted || m.posting }

func (m *Model) applySubmit(msg submittedMsg) {
	m.posting, m.failure = false, msg.err
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	m.posted = true
	m.say(msg.summary+", press q to leave", false)
}

func (m *Model) rebuild() {
	m.screen = build(m.review, m.diff, m.width)
	m.cursor = clamp(m.cursor, len(m.screen.rows)-1)
	m.follow()
}

func (m *Model) moveBy(n int) {
	m.cursor = clamp(m.cursor+n, len(m.screen.rows)-1)
}

// jump moves to the next row matching want and anchors it near the top of the
// frame. Scrolling by the least that reaches a heading leaves it on the last
// line, which is the one place the content under it cannot be read.
func (m *Model) jump(step int, want func(row) bool) bool {
	for i := m.cursor + step; i >= 0 && i < len(m.screen.rows); i += step {
		if want(m.screen.rows[i]) {
			m.cursor = i
			m.reveal()

			break
		}
	}

	return true
}

// reveal anchors the cursor near the top of the frame, stopping at the last
// full frame of rows so the end of the review is not scrolled into blankness.
func (m *Model) reveal() {
	h := m.viewHeight()
	m.offset = clamp(m.cursor-jumpMargin, len(m.screen.rows)-h)
}

// follow keeps the cursor on screen with a few rows of context either side,
// scrolling by the smallest amount that gets there. Landing on a comment
// reveals the rest of it where the frame has room, since a comment's first
// line is its severity and the sentence under it is the part worth reading.
func (m *Model) follow() {
	// Half a frame is the most a margin can be before the two bounds below
	// fight each other and the view flips on every keystroke.
	const sides = 2

	h := m.viewHeight()
	pad := min(scrollOff, (h-1)/sides)

	m.offset = min(m.offset, m.cursor-pad)
	m.offset = max(m.offset, min(m.blockEnd()+pad, m.cursor+h-1)-h+1)
	m.offset = clamp(m.offset, len(m.screen.rows)-h)
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
	const title = 1

	return max(1, m.height-title-len(m.footerLines()))
}

// clamp holds v inside [0, hi], which is every bound a row index has.
func clamp(v, hi int) int {
	return max(0, min(v, max(0, hi)))
}
