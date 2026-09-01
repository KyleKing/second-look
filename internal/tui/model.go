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
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/shellrun"
	"github.com/kyleking/second-look/internal/structure"
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
	read    *seen.Set
	seenAt  string
	path    string
	submit  Submitter
	send    Sender
	merge   Merger
	tree    Tree

	screen screen
	cursor int
	offset int
	width  int
	height int

	keys   keyMap
	styles styles
	search search

	// pending is the ] or [ waiting for the object that completes it.
	pending rune
	// last is the motion n repeats and N reverses, and change is the keystroke
	// . replays. Only a change that needs no further input is recorded, since
	// replaying an editor blind is not a repeat of anything.
	last   *motion
	change *tea.KeyPressMsg

	status    string
	failed    bool
	posted    bool
	posting   bool
	merged    bool
	merging   bool
	asking    confirmKind
	searching bool
	view      viewMode
	// editing is the in-place editor, nil when nothing is being written.
	editing *editor
	fold    foldLevel
	// cosmetic is the structural pass over every hunk, nil until it answers.
	cosmetic map[hunkAt]bool
	// cost is what the same pass rates the change, shown in the title once it
	// has an answer to show.
	cost    rate.Score
	reading bool
	// folded is what z has put away by hand.
	folded folded
	help   bool
	// checkout is C, answered by the caller once the screen has closed.
	checkout bool
	// failure is the last submit that did not post, cleared by one that does.
	// The screen leaves through it, so a run that failed to post says so on
	// stdout and in the exit code rather than only in a footer nobody kept.
	failure error
}

// Option configures the review screen.
type Option func(*Model)

// WithSeen carries which hunks have already been read, and where to write that
// back. Without it the screen still runs and space says there is nowhere to
// record an answer, rather than pretending to remember one.
func WithSeen(read *seen.Set, path string) Option {
	return func(m *Model) { m.read, m.seenAt = read, path }
}

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
		keys: defaultKeyMap(), styles: newStyles(), search: newSearch(),
		width: minWidth, height: startHeight, folded: newFolded(),
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

	// The pass costs a subprocess per hunk side, so it runs behind the first
	// frame rather than in front of it: the rating appears when it is ready and
	// t is a redraw by the time anyone presses it.
	if !structure.Available() {
		return nil
	}

	return readStructure(m.diff)
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

type sentMsg struct {
	id      string
	summary string
	err     error
}

type submittedMsg struct {
	summary string
	err     error
}

// mergedMsg is what the merge answered with.
type mergedMsg struct {
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
	case sentMsg:
		m.applySent(msg)

		return m, tea.ClearScreen
	case structureMsg:
		asked := m.reading
		m.reading = false

		if msg.err != nil {
			// A pass nobody asked for fails quietly: the rating is missing from
			// the title, which is the whole of what it was for.
			if asked {
				m.say("reading the structure: "+msg.err.Error(), true)
			}

			return m, nil
		}

		m.cosmetic, m.cost = msg.cosmetic, msg.score

		if asked {
			m.setFold(foldCosmetic)
		}

		return m, nil
	case mergedMsg:
		m.applyMerge(msg)

		return m, tea.ClearScreen
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
	// A prompt, a confirmation, and a half-typed motion each own the keyboard
	// until they are finished, so they are answered before anything else.
	switch {
	case m.editing != nil:
		return m.typeBody(msg)
	case m.asking != askNothing:
		return m.answer(msg)
	case m.searching:
		return m.typing(msg)
	case m.pending != 0:
		return m.complete(msg)
	}

	if handled, model, cmd := m.mode(msg); handled {
		return model, cmd
	}

	if m.moved(msg) {
		return m, nil
	}

	// The state keys sit behind m now. A bare press is answered rather than
	// ignored, because the hand that learned them reaches for them for weeks.
	if m.stateKey(msg) {
		m.say("m first: m r ready · m d draft · m x skip", false)

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

// mode handles the keys that change what the screen is showing rather than what
// the review says, and reports whether one of them matched.
func (m *Model) mode(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.help {
			m.help = false

			return true, m, nil
		}

		return true, m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help = !m.help
	case key.Matches(msg, m.keys.List):
		m.cycleView()
	case key.Matches(msg, m.keys.Fold):
		m.setFold(foldWhitespace)
	case key.Matches(msg, m.keys.Structure):
		cmd := m.askStructure()

		return true, m, cmd
	case key.Matches(msg, m.keys.Zed):
		m.pending = 'z'

		m.say(m.chord("z", foldObjects()), false)
	case key.Matches(msg, m.keys.State):
		m.pending = 'm'

		m.say(m.chord("m", states()), false)
	case key.Matches(msg, m.keys.Search):
		cmd := m.begin()

		return true, m, cmd
	case key.Matches(msg, m.keys.Forward), key.Matches(msg, m.keys.Backward):
		m.pending = ']'
		if key.Matches(msg, m.keys.Backward) {
			m.pending = '['
		}

		m.say(m.chord(string(m.pending), objects()), false)
	default:
		return false, m, nil
	}

	return true, m, nil
}

// records marks the changes the repeat key can replay: the ones that take no
// further input, so replaying one does exactly what it did the first time.
func (m *Model) records(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Seen)
}

// complete answers the second key of a chord. An unknown letter cancels rather
// than waiting, since a half-typed chord that swallows the next keystroke is
// worse than one that says it did not land.
func (m *Model) complete(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	prefix := m.pending
	m.pending = 0

	if key.Matches(msg, m.keys.Quit) {
		m.say("", false)

		return m, nil
	}

	switch prefix {
	case 'z':
		m.foldNote(msg)

		return m, nil
	case 'm':
		if !m.stateKey(msg) {
			m.say("no state for "+msg.String()+"; r ready, d draft, x skip", false)

			return m, nil
		}

		replay := msg
		m.change = &replay

		return m.act(msg)
	}

	m.object(prefix, msg)

	return m, nil
}

// stateKey reports whether a key names one of the three comment states.
func (m *Model) stateKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Ready) ||
		key.Matches(msg, m.keys.Draft) ||
		key.Matches(msg, m.keys.Skip)
}

// foldNote answers the z chord: za inverts what the cursor is standing on, zo
// and zc name a direction, and zR and zM answer for the whole review.
func (m *Model) foldNote(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "a":
		m.foldHere(!m.openHere())
	case "o":
		m.foldHere(true)
	case "c":
		m.foldHere(false)
	case "R":
		m.foldAll(true)
	case "M":
		m.foldAll(false)
	default:
		m.say("no fold for "+msg.String()+"; a this one, R open all, M close all", false)
	}
}

// foldHere folds what the cursor is on, which is the file from its name, the
// hunk from anywhere inside it, and the note from the comment it belongs to.
func (m *Model) foldHere(open bool) {
	r := m.screen.rows[m.cursor]

	switch {
	case r.kind == rowFile && r.path != "":
		m.folded.files[r.path] = !open
	case r.hunk > 0:
		m.folded.hunks[hunkAt{r.path, r.hunk}] = !open
	default:
		if !m.setNote(r.comment, open) {
			return
		}
	}

	m.rebuild()
	m.say("", false)
}

// openHere is whether what the cursor is on is showing, which is what za has
// to invert.
func (m *Model) openHere() bool {
	r := m.screen.rows[m.cursor]

	switch {
	case r.kind == rowFile:
		return !m.folded.files[r.path]
	case r.hunk > 0:
		return !m.folded.hunks[hunkAt{r.path, r.hunk}]
	}

	return m.expanded(r.comment)
}

// setNote records the fold for the block a comment owns and reports whether
// there was anything to fold. The code view folds the comment itself, so there
// always is; every other view folds the note under it. Answering a keystroke
// with a frame that did not change reads as a key that is not working.
func (m *Model) setNote(index int, open bool) bool {
	switch {
	case index == noComment:
		m.say("nothing to fold here", false)

		return false
	case index >= 0 && m.view != viewCode && m.review.Comments[index].Note == "":
		m.say("no note on "+m.review.Comments[index].ID, false)

		return false
	case index == reviewBody && m.review.Body == "",
		index == reviewNote && m.review.Note == "":
		m.say("nothing written here yet; e writes it", false)

		return false
	}

	m.folded.notes[index] = open

	return true
}

// foldAll answers zR and zM. Closing everything leaves the file names and what
// each carries, which is the outline a long review is read from.
func (m *Model) foldAll(open bool) {
	m.folded = newFolded()

	for i := range m.review.Comments {
		m.folded.notes[i] = open
	}

	m.folded.notes[reviewBody], m.folded.notes[reviewNote] = open, open

	if !open {
		for i := range m.diff.Files {
			m.folded.files[filePath(&m.diff.Files[i])] = true
		}
	}

	m.rebuild()
	m.say(foldedWord(open), false)
}

func foldedWord(open bool) string {
	if open {
		return "everything open"
	}

	return "folded to the file names"
}

// expanded reports whether the block under the cursor is drawn in full. It is
// read off the frame rather than recomputed, so za always inverts what the eye
// is looking at.
func (m *Model) expanded(index int) bool {
	for _, r := range m.screen.rows {
		if r.comment == index && r.folded {
			return false
		}
	}

	return true
}

// object completes a pending ] or [.
func (m *Model) object(prefix rune, msg tea.KeyPressMsg) {
	step := 1
	if prefix == '[' {
		step = -1
	}

	switch msg.String() {
	case "h":
		m.repeatable(motion{step, "hunk", isHunk})
	case "f":
		m.repeatable(motion{step, "file", isKind(rowFile)})
	case "d":
		m.repeatable(motion{step, "directory", isKind(rowGroup)})
	case "c":
		m.repeatable(motion{step, "comment", isComment})
	case "t":
		m.repeatable(motion{step, "thread", isThread})
	case "u":
		m.repeatable(motion{step, "unread hunk", m.isUnread})
	default:
		m.say("no motion for "+msg.String(), false)
	}
}

// repeatable runs a motion and remembers it, so n walks the same object.
func (m *Model) repeatable(mo motion) {
	m.last = &mo
	m.jump(mo.step, mo.what, mo.want)
}

// chord is what the second key can be, shown while the first is waiting. The
// brackets are unstyled because the footer renders the status as one span, and
// a color opened inside it would close the span around it.
func (*Model) chord(prefix string, hints [][2]string) string {
	return prefix + hintGap + hintLine(styles{}, hints)
}

// moved handles every key that only changes where the cursor is, so the action
// keys below stay a flat list of things that change the review.
func (m *Model) moved(msg tea.KeyPressMsg) bool {
	const halfPage = 2

	half := m.viewHeight() / halfPage

	// A peek moves the frame and leaves the cursor where it was, so a glance at
	// what is above or below costs nothing to come back from: the next motion
	// pulls the frame to the cursor before it moves.
	switch {
	case key.Matches(msg, m.keys.PeekDown):
		return m.peek(1)
	case key.Matches(msg, m.keys.PeekUp):
		return m.peek(-1)
	}

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

// peek scrolls the frame by a line without moving the cursor.
func (m *Model) peek(step int) bool {
	m.offset = clamp(m.offset+step, len(m.screen.rows)-m.viewHeight())
	m.say("", false)

	return true
}

// cycleView walks the three views, keeping the cursor on the same comment
// across the change. Losing your place is what makes another view a detour
// rather than a shortcut.
func (m *Model) cycleView() {
	was := m.current()
	m.view = m.view.next()
	m.rebuild()

	m.cursor = 0
	m.reveal()
	m.say("", false)

	if was < 0 {
		m.say(m.view.String(), false)

		return
	}

	if !m.focus(was) {
		// The only comment the list leaves out is a skipped one, and landing
		// silently at the top would read as the cursor having been lost.
		m.say("skipped comments are counted here, not listed", false)
	}
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

// markRead flips the hunk under the cursor, or every hunk of the file when the
// cursor is on a file line, which is the one place "the whole thing" is
// unambiguous.
func (m *Model) markRead() {
	if m.read == nil {
		m.say("nowhere to record what has been read", true)

		return
	}

	r := m.screen.rows[m.cursor]

	if r.kind == rowFile {
		refs := m.hunksOf(r.path)
		if len(refs) == 0 {
			m.say("nothing to read in "+r.path, false)

			return
		}

		read := !m.allRead(refs)
		ids := make([]seen.ID, 0, len(refs))

		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}

		m.read.Mark(read, ids...)
		m.saveRead(r.path + ": " + plural(len(refs), "hunk") + " " + readWord(read))

		return
	}

	if r.hunk == 0 {
		m.say("no hunk here", false)

		return
	}

	read := m.read.Toggle(seen.Hunk(m.diff, r.path, r.hunk))
	m.saveRead("hunk " + readWord(read))
}

func readWord(read bool) string {
	if read {
		return "read"
	}

	return "unread"
}

// shownHunks is every hunk the frame is currently showing, which is every hunk
// unless whitespace-only ones are folded away.
func (m *Model) shownHunks() []seen.Ref {
	skip := m.skipper().skip
	if skip == nil {
		return seen.Hunks(m.diff)
	}

	all := seen.Hunks(m.diff)
	out := make([]seen.Ref, 0, len(all))

	for _, r := range all {
		if !skip(r.Path, r.Hunk) {
			out = append(out, r)
		}
	}

	return out
}

// setFold turns a level on, or off when it is the one already showing.
func (m *Model) setFold(want foldLevel) {
	if m.fold == want {
		want = foldNone
	}

	m.fold = want
	m.rebuild()
	m.say(foldWord(m.fold), false)
}

// askStructure answers t: it reads the diff once, keeps the answer, and says so
// where the tool that carries the grammars is missing.
func (m *Model) askStructure() tea.Cmd {
	if m.fold == foldCosmetic || m.cosmetic != nil {
		m.setFold(foldCosmetic)

		return nil
	}

	if m.reading {
		m.say("still reading the structure", false)

		return nil
	}

	if !structure.Available() {
		m.say(errNoParser.Error(), true)

		return nil
	}

	m.reading = true
	m.say("reading the structure of "+plural(len(seen.Hunks(m.diff)), "hunk")+"...", false)

	return readStructure(m.diff)
}

func (m *Model) hunksOf(path string) []seen.Ref {
	var out []seen.Ref

	for _, ref := range seen.Hunks(m.diff) {
		if ref.Path == path {
			out = append(out, ref)
		}
	}

	return out
}

func (m *Model) allRead(refs []seen.Ref) bool {
	for _, ref := range refs {
		if !m.read.Has(ref.ID) {
			return false
		}
	}

	return true
}

// saveRead writes the set through immediately, the way every other change is
// written, so quitting loses nothing.
func (m *Model) saveRead(ok string) {
	if err := seen.Save(m.seenAt, m.read, seen.Hunks(m.diff)); err != nil {
		m.say(err.Error(), true)

		return
	}

	m.rebuild()
	m.say(ok, false)
}

// isUnread accepts a hunk heading nobody has marked read, which is what makes
// ]u the motion that walks what is left to do.
func (m *Model) isUnread(r row) bool {
	if r.kind != rowHunk || r.hunk == 0 || m.read == nil {
		return false
	}

	return !m.read.Has(seen.Hunk(m.diff, r.path, r.hunk))
}

// isHunk accepts a real @@ heading. The review's own body and note, and the
// line naming a rename or a binary payload, share the heading style without
// being hunks, so a motion over hunks has to look past the style.
func isHunk(r row) bool { return r.kind == rowHunk && r.hunk > 0 }

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
	case key.Matches(msg, m.keys.Seen):
		m.markRead()
	case key.Matches(msg, m.keys.Send):
		cmd := m.sendOne()

		return m, cmd
	case key.Matches(msg, m.keys.Shell):
		cmd := m.shell()

		return m, cmd
	case key.Matches(msg, m.keys.Checkout):
		cmd := m.wantCheckout()

		return m, cmd
	case key.Matches(msg, m.keys.Note):
		cmd := m.editNote()

		return m, cmd
	case key.Matches(msg, m.keys.Edit):
		cmd := m.edit()

		return m, cmd
	case key.Matches(msg, m.keys.Submit):
		m.askSubmit()
	case key.Matches(msg, m.keys.Merge):
		m.askMergeNow()
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
// it writes the reply to it, since a posted comment cannot be changed; and on
// the review's own body or note it edits the review's prose, which is the one
// thing on this screen that used to be editable only in the file.
func (m *Model) edit() tea.Cmd {
	if t := m.currentThread(); t >= 0 {
		return m.beginEdit("replying to @"+m.threads[t].Notes[0].Author,
			"", editedMsg{index: -1, replyTo: t})
	}

	switch i := m.current(); i {
	case reviewBody:
		return m.beginEdit("editing the review body", m.review.Body,
			editedMsg{index: reviewBody, replyTo: -1, field: fieldBody})
	case reviewNote:
		return m.beginEdit("editing the review note, which stays local", m.review.Note,
			editedMsg{index: reviewNote, replyTo: -1, field: fieldNote})
	case noComment:
		m.say("no comment here", false)

		return nil
	default:
		return m.beginEdit("editing "+m.review.Comments[i].ID, m.review.Comments[i].Body,
			editedMsg{index: i, replyTo: -1, field: fieldBody})
	}
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

	return m.beginEdit("editing the note on "+m.review.Comments[i].ID,
		m.review.Comments[i].Note, editedMsg{index: i, replyTo: -1, field: fieldNote})
}

// shell hands the terminal to $SHELL in the repository and appends what the
// session printed to the note under the cursor. Running the code under review
// and then writing the comment is the flow this exists for, and a transcript is
// what makes the comment evidence rather than a claim.
// C leaves the screen so the working copy can be moved onto the pull request,
// which is the one thing reviewing from the API cannot supply.
func (m *Model) wantCheckout() tea.Cmd {
	switch m.tree {
	case TreeOnHead:
		m.say("the checkout is already on this pull request", false)

		return nil
	case TreeNone:
		m.say("no checkout of "+m.review.Owner+"/"+m.review.Repo+" here; clone it first", true)

		return nil
	case TreeElsewhere:
	}

	m.checkout = true

	return tea.Quit
}

// noTree is why the shell has nowhere useful to run, which is worth naming
// rather than opening one against whatever the working directory happens to be.
func (m *Model) noTree() string {
	if m.tree == TreeNone {
		return "no checkout of " + m.review.Owner + "/" + m.review.Repo +
			" here, so a shell would run somewhere else"
	}

	return "the checkout is on another branch; C moves it onto this pull request"
}

func (m *Model) shell() tea.Cmd {
	if m.tree != TreeOnHead {
		m.say(m.noTree(), true)

		return nil
	}

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

	// A review's body is emptied deliberately; a comment's body left empty is
	// an editor closed without saving, which is a cancel rather than a change.
	if msg.body == "" && msg.field == fieldBody && msg.index != reviewBody {
		m.say("empty body, nothing changed", false)

		return
	}

	switch msg.index {
	case reviewBody:
		m.review.Body = msg.body
		m.save("review body updated")

		return
	case reviewNote:
		m.review.Note = msg.body
		m.save("review note updated; it stays local")

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

// sendOne posts the comment under the cursor on its own. It asks nothing first:
// a single comment is the one thing here small enough to take back by deleting
// it on GitHub, where a whole review is not.
func (m *Model) sendOne() tea.Cmd {
	i := m.current()
	if i < 0 {
		m.say("no comment here", false)

		return nil
	}

	if m.send == nil {
		m.say("this screen cannot post a single comment", true)

		return nil
	}

	if m.review.Comments[i].Status != artifact.StatusReady {
		m.say("only a ready comment goes out on its own; r marks it ready", false)

		return nil
	}

	ctx, review, id := m.ctx, m.review, m.review.Comments[i].ID
	m.say("posting "+id+"…", false)

	return func() tea.Msg {
		summary, err := m.send(ctx, review, id)

		return sentMsg{id: id, summary: summary, err: err}
	}
}

func (m *Model) applySent(msg sentMsg) {
	if msg.err != nil {
		m.failure = msg.err
		m.say(msg.err.Error(), true)

		return
	}

	m.rebuild()
	m.say(msg.summary, false)
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
		_ = m.focus(m.firstDraft())
		m.say(plural(c.draft, "comment")+" still draft, m r posts it or m x skips it", true)

		return
	}

	m.asking = askSubmit
	// The pull request is already named in the title bar, so the prompt spends
	// its width on what the keys do and stays readable in an 80-column frame.
	m.say("S again to post, any key cancels: "+plural(c.ready, "comment")+" as "+m.event(), false)
}

// confirmKind is which confirmation owns the keyboard. Two of the screen's keys
// send something that cannot be taken back, and each is confirmed by its own
// key rather than by any keystroke at all.
type confirmKind int

const (
	askNothing confirmKind = iota
	askSubmit
	askMerge
)

// askMergeNow asks before it merges. A merge is the least reversible thing this
// screen does, and a review still staged is work the merge would strand, so it
// is refused rather than confirmed.
func (m *Model) askMergeNow() {
	switch {
	case m.merge == nil:
		m.say("merging is not available here", true)
	case m.merging:
		m.say("merging…", false)
	case m.merged:
		m.say("already merged", false)
	case m.posting:
		m.say("the review is still posting", false)
	case !m.posted && !m.review.Empty():
		m.say("this review is still staged; S posts it, or skip its comments first", true)
	default:
		m.asking = askMerge
		m.say("M again to squash-merge and delete the branch, any key cancels", false)
	}
}

// answer reads the reply to a prompt. Anything but the same key again cancels
// and is swallowed, so no keystroke meant for the review sends anything.
func (m *Model) answer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	kind := m.asking
	m.asking = askNothing

	if kind == askMerge {
		if !key.Matches(msg, m.keys.Merge) {
			m.say("canceled, nothing was merged", false)

			return m, nil
		}

		m.merging = true

		m.say("merging…", false)

		ctx, review := m.ctx, m.review

		return m, func() tea.Msg {
			summary, err := m.merge(ctx, review)

			return mergedMsg{summary: summary, err: err}
		}
	}

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
func (m *Model) focus(index int) bool {
	for i, r := range m.screen.rows {
		if r.head && r.comment == index {
			m.cursor = i
			m.reveal()

			return true
		}
	}

	return false
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

// applyMerge reports the merge. A failure is carried out of the screen the way
// a failed post is, since a merge that GitHub refused is the one thing a
// footer taken away by the alternate screen must not swallow.
func (m *Model) applyMerge(msg mergedMsg) {
	m.merging, m.failure = false, msg.err
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	m.merged = true
	m.say(msg.summary+", press q to leave", false)
}

func (m *Model) rebuild() {
	lay := layout{width: m.width, hide: m.skipper(), fold: m.folded}

	switch m.view {
	case viewComments:
		m.screen = buildList(m.review, m.diff, lay)
	case viewCode:
		m.screen = buildCode(m.review, m.diff, m.threads, lay)
	case viewDiff:
		m.screen = build(m.review, m.diff, m.threads, lay)
	}

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
//
// A comment block opens with a blank row, so landing on one keeps a row more:
// the line a comment is about is the context it cannot be read without.
func (m *Model) reveal() {
	margin := jumpMargin
	if m.cursor > 0 && m.screen.rows[m.cursor-1].kind == rowBlank {
		margin++
	}

	h := m.viewHeight()
	m.offset = clamp(m.cursor-margin, len(m.screen.rows)-h)
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
