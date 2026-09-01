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
	"github.com/kyleking/second-look/internal/shellrun"
	"github.com/kyleking/second-look/internal/threads"
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
	ctx     context.Context //nolint:containedctx // it bounds the editor subprocess and the post
	review  *artifact.Review
	diff    *diff.Diff
	threads []threads.Thread
	path    string
	submit  Submitter

	screen screen
	cursor int
	offset int
	width  int
	height int

	keys   keyMap
	styles styles

	// pending is the ] or [ waiting for the object that completes it.
	pending rune
	// last is the motion n repeats and N reverses, and change is the keystroke
	// . replays. Only a change that needs no further input is recorded, since
	// replaying an editor blind is not a repeat of anything.
	last   *motion
	change *tea.KeyPressMsg

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

// Option configures the review screen.
type Option func(*Model)

// WithThreads shows the conversations already open on the pull request, which
// `second-look get` cached. Without it the screen shows the diff and the
// prepared review alone, which is what a review staged before threads were
// fetched has.
func WithThreads(ts []threads.Thread) Option {
	return func(m *Model) { m.threads = ts }
}

// New builds the review screen for a prepared review and the diff it was
// staged against.
func New(
	ctx context.Context, r *artifact.Review, d *diff.Diff,
	path string, submit Submitter, opts ...Option,
) *Model {
	m := &Model{
		ctx: ctx, review: r, diff: d, path: path, submit: submit,
		keys: defaultKeyMap(), styles: newStyles(),
		width: minWidth, height: startHeight,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Init lays out the first frame at the assumed size, which the terminal
// corrects with a resize before anything is drawn.
func (m *Model) Init() tea.Cmd {
	m.rebuild()

	return nil
}

// field names which half of a comment an edit came back for. The note is local
// and never posted, so the two cannot share a write.
type field int

const (
	fieldBody field = iota
	fieldNote
)

type editedMsg struct {
	// index is the comment the edit replaces, or -1 for a reply, which stages a
	// comment that did not exist when the editor opened.
	index   int
	replyTo int
	field   field
	body    string
	err     error
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

// motion is a direction and what to stop on, kept so n can repeat it.
type motion struct {
	step int
	what string
	want func(row) bool
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirming {
		return m.answer(msg)
	}

	if m.pending != 0 {
		m.object(msg)

		return m, nil
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

	if key.Matches(msg, m.keys.Forward) || key.Matches(msg, m.keys.Backward) {
		m.pending = ']'
		if key.Matches(msg, m.keys.Backward) {
			m.pending = '['
		}

		m.say(objectMenu(m.pending), false)

		return m, nil
	}

	if m.moved(msg) {
		return m, nil
	}

	if key.Matches(msg, m.keys.Repeat) {
		if m.change == nil {
			m.say("nothing to repeat", false)

			return m, nil
		}

		return m.act(*m.change)
	}

	if m.records(msg) {
		replay := msg
		m.change = &replay
	}

	return m.act(msg)
}

// records marks the changes the repeat key can replay: the ones that take no
// further input, so replaying one does exactly what it did the first time.
func (m *Model) records(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Ready) ||
		key.Matches(msg, m.keys.Draft) ||
		key.Matches(msg, m.keys.Skip)
}

// object completes a pending ] or [. An unknown letter cancels rather than
// waiting, since a half-typed motion that swallows the next keystroke is worse
// than one that says it did not land.
func (m *Model) object(msg tea.KeyPressMsg) {
	step := 1
	if m.pending == '[' {
		step = -1
	}

	m.pending = 0

	if key.Matches(msg, m.keys.Quit) {
		m.say("", false)

		return
	}

	switch msg.String() {
	case "h":
		m.repeatable(motion{step, "hunk", isKind(rowHunk)})
	case "f":
		m.repeatable(motion{step, "file", isKind(rowFile)})
	case "c":
		m.repeatable(motion{step, "comment", isComment})
	case "t":
		m.repeatable(motion{step, "thread", isThread})
	default:
		m.say("no motion for "+msg.String(), false)
	}
}

// repeatable runs a motion and remembers it, so n walks the same object.
func (m *Model) repeatable(mo motion) {
	m.last = &mo
	m.jump(mo.step, mo.what, mo.want)
}

func objectMenu(prefix rune) string {
	parts := make([]string, 0, len(objects()))
	for _, o := range objects() {
		parts = append(parts, o[0]+" "+o[1])
	}

	return string(prefix) + "  " + strings.Join(parts, "   ")
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
	case key.Matches(msg, m.keys.NextNote):
		m.repeatable(motion{1, "comment or thread", isHead})
	case key.Matches(msg, m.keys.PrevNote):
		m.repeatable(motion{-1, "comment or thread", isHead})
	case key.Matches(msg, m.keys.Again):
		return m.again(1)
	case key.Matches(msg, m.keys.Reverse):
		return m.again(-1)
	default:
		return false
	}

	m.say("", false)
	m.follow()

	return true
}

// again repeats the last motion, in its own direction or reversed. It is what
// makes a two-key motion cost two keys once rather than every time.
func (m *Model) again(direction int) bool {
	if m.last == nil {
		m.say("no motion to repeat; ] names one", false)

		return true
	}

	m.jump(m.last.step*direction, m.last.what, m.last.want)

	return true
}

func isComment(r row) bool { return r.head && r.kind == rowComment && r.comment >= 0 }

func isThread(r row) bool { return r.head && r.kind == rowThread }

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
	case key.Matches(msg, m.keys.Shell):
		cmd := m.shell()

		return m, cmd
	case key.Matches(msg, m.keys.Note):
		cmd := m.editNote()

		return m, cmd
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

// edit opens $EDITOR on whatever the cursor is standing on, which owns the
// terminal until it exits, following the same rule as running the code under
// review. On a prepared comment it edits the body; on a thread already posted
// it writes the reply to it, since a posted comment cannot be changed.
func (m *Model) edit() tea.Cmd {
	if t := m.currentThread(); t >= 0 {
		return m.open("", editedMsg{index: -1, replyTo: t})
	}

	i := m.current()
	if i < 0 {
		m.say("no comment here", false)

		return nil
	}

	return m.open(m.review.Comments[i].Body, editedMsg{index: i, replyTo: -1, field: fieldBody})
}

// editNote opens the local note, which is where the evidence for a comment
// lives: what was run, what it printed, why the doubt stands. It never posts,
// so it is edited apart from the body rather than alongside it.
func (m *Model) editNote() tea.Cmd {
	i := m.current()
	if i < 0 {
		m.say("no comment here", false)

		return nil
	}

	return m.open(m.review.Comments[i].Note, editedMsg{index: i, replyTo: -1, field: fieldNote})
}

// shell hands the terminal to $SHELL in the repository and appends what the
// session printed to the note under the cursor. Running the code under review
// and then writing the comment is the flow this exists for, and a transcript is
// what makes the comment evidence rather than a claim.
func (m *Model) shell() tea.Cmd {
	i := m.current()
	if i < 0 {
		m.say("no comment here; the transcript attaches to one", false)

		return nil
	}

	file, err := os.CreateTemp("", "second-look-*.transcript")
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	name := file.Name()
	if err := file.Close(); err != nil {
		m.say(err.Error(), true)

		return nil
	}

	cmd, err := shellrun.Capture(m.ctx, name, shellrun.Shell())
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		//nolint:errcheck // a temp file that outlives the session is not worth an error path
		defer os.Remove(name)

		if err != nil {
			return editedMsg{index: i, replyTo: -1, field: fieldNote, err: err}
		}

		raw, err := os.ReadFile(name) //nolint:gosec // our own temp file
		if err != nil {
			return editedMsg{index: i, replyTo: -1, field: fieldNote, err: err}
		}

		return editedMsg{
			index: i, replyTo: -1, field: fieldNote,
			body: appendTranscript(m.review.Comments[i].Note, shellrun.Clean(raw)),
		}
	})
}

// appendTranscript keeps what the note already said. A second session is more
// evidence, not a correction of the first.
func appendTranscript(note, transcript string) string {
	if transcript == "" {
		return note
	}

	if note == "" {
		return transcript
	}

	return note + "\n\n" + transcript
}

// open puts start in a temporary file, hands the terminal to $EDITOR, and
// returns what came back shaped as msg.
func (m *Model) open(start string, msg editedMsg) tea.Cmd {
	file, err := os.CreateTemp("", "second-look-*.md")
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	name := file.Name()
	if _, err := file.WriteString(start); err != nil {
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
			msg.err = err

			return msg
		}

		body, err := os.ReadFile(name) //nolint:gosec // our own temp file
		if err != nil {
			msg.err = err

			return msg
		}

		msg.body = strings.TrimRight(string(body), "\n")

		return msg
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

	if msg.body == "" && msg.field == fieldBody {
		m.say("empty body, nothing changed", false)

		return
	}

	if msg.index < 0 {
		m.stageReply(msg)

		return
	}

	c := &m.review.Comments[msg.index]

	if msg.field == fieldNote {
		if c.Note == msg.body {
			m.say("unchanged", false)

			return
		}

		c.Note = msg.body
		m.save("note on " + c.ID + " updated; it stays local")

		return
	}

	if c.Body == msg.body {
		m.say("unchanged", false)

		return
	}

	c.Body = msg.body
	m.save("edited " + c.ID)
}

// stageReply puts an answer to an open thread into the prepared review. It is
// staged ready rather than draft: a draft is a comment nobody has ruled on, and
// this one was typed by the person the screen belongs to.
func (m *Model) stageReply(msg editedMsg) {
	if msg.replyTo < 0 || msg.replyTo >= len(m.threads) {
		return
	}

	t := &m.threads[msg.replyTo]

	id := fmt.Sprintf("reply-%d", t.ReplyTo())
	for i := range m.review.Comments {
		if m.review.Comments[i].ID == id && m.review.Comments[i].Body == msg.body {
			m.say("unchanged", false)

			return
		}
	}

	m.review.Upsert(artifact.Comment{
		ID: id, Path: t.Path, Side: t.Side, Line: t.Line,
		InReplyTo: t.ReplyTo(), Body: msg.body, Severity: "question", Status: artifact.StatusReady,
	})

	m.save(fmt.Sprintf("reply to %d staged, ready to post", t.ReplyTo()))
}

// currentThread is the open thread the cursor is standing in, or -1.
func (m *Model) currentThread() int {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return -1
	}

	r := m.screen.rows[m.cursor]
	if r.kind != rowThread {
		return -1
	}

	return r.thread
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
	m.screen = build(m.review, m.diff, m.threads, m.width)
	m.cursor = clamp(m.cursor, len(m.screen.rows)-1)
	m.follow()
}

func (m *Model) moveBy(n int) {
	m.cursor = clamp(m.cursor+n, len(m.screen.rows)-1)
}

// jump moves to the next row matching want and anchors it near the top of the
// frame. Scrolling by the least that reaches a heading leaves it on the last
// line, which is the one place the content under it cannot be read.
//
// Running out of matches says so, because a key that silently does nothing
// reads as a key that is not working, and the end of a review is where one more
// press is most likely.
func (m *Model) jump(step int, what string, want func(row) bool) {
	for i := m.cursor + step; i >= 0 && i < len(m.screen.rows); i += step {
		if want(m.screen.rows[i]) {
			m.cursor = i
			m.say("", false)
			m.reveal()

			return
		}
	}

	where := "after"
	if step < 0 {
		where = "before"
	}

	m.say(fmt.Sprintf("no %s %s this one", what, where), false)
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
