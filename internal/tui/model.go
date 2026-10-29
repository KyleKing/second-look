package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/kyleking/aragonite/tui/handoff"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/overlay"
	"github.com/kyleking/aragonite/tui/pane"

	"github.com/kyleking/second-look/internal/acp"
	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/ghmd"
	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/journal"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/shellrun"
	"github.com/kyleking/second-look/internal/structure"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/versions"
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
	scrollOff = 3
	// Overscroll is how far past the last row a deliberate scroll may sit, so
	// the last line need not be pinned to the edge. A motion pulls it back.
	overscroll = 3
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
	// notes is those threads' comments segmented once. Their bodies never
	// change, and a rebuild happens on every keystroke.
	notes map[noteAt][]ghmd.Block
	// lit is every fenced code block inside a note, lexed the first time it is
	// drawn and kept after that, the same way lexed is for the diff's own hunks.
	lit    map[string][][]highlight.Span
	read   *seen.Set
	seenAt string
	path   string
	// store is the directory this review's caches live in, which is where the
	// rating is left for the queue to read.
	store string
	// workRoot is the working copy the review was taken in, set only where
	// writing into it is the review's business — a local review's own tree —
	// and empty otherwise, which is what F checks.
	workRoot string
	// repoDir is a checkout of the repository, empty where there is none.
	repoDir string
	// agentProbe asks what the review's recorded session is doing, and
	// agentState is its last answer. A session the listing does not name is an
	// ended run, so the title says nothing rather than a stale word.
	agentProbe AgentProbe
	agentState string
	// dispatcher hands the todo set to an agent, and is nil where nothing is
	// configured to receive it.
	dispatcher Dispatcher
	// asker puts a one-shot question to the configured agent and takes its
	// answer as text, which is the dispatch argv's other shape.
	asker Asker
	// agentStart opens an agent session over ACP, and agent is the live one.
	// agentOpen is the pane its transcript draws in, which esc leaves without
	// ending the session; agentDead is how it ended, agentOff how far back
	// the transcript is scrolled, and agentSaw whether a turn has answered.
	agentStart AgentStarter
	agent      *acp.Session
	agentOpen  bool
	agentDead  error
	agentOff   int
	agentSaw   bool
	agentIn    textinput.Model
	// draftText overrides the instruction T sends an agent over an empty
	// review, where a config already has its own skill to point at by name.
	draftText string
	restage   Restager
	// left is where the diff's cursor stood when a view off the cycle opened, which closing it returns to.
	left where
	// headSearch finds files at head carrying a line, and atHead is what each
	// shape chapter's search found, keyed by the line.
	headSearch HeadSearch
	headList   HeadList
	atHead     map[string]headHits
	// drafting is a question being typed about a range, inFlight is who each ask
	// still being answered was put to by id, and failedAsks why one got nothing.
	drafting   *askDraft
	inFlight   map[string]string
	failedAsks map[string]string
	// around is how many lines of the file either side of a hunk the reader
	// asked for, blobs is the files read to answer that, and blob is what reads
	// one. blob is nil where nothing can.
	around map[hunkAt]int
	blobs  map[string][]string
	blob   Blobs
	// onlyNew hides every hunk already marked read, which is the second pass
	// over a pull request that moved under an earlier one.
	onlyNew bool
	// since is every hunk an earlier round already carried, and sinceSHA is the
	// round it came from. Both are nil until a round is picked, and it is a
	// separate axis from onlyNew: what a round held and what this pass marked
	// read are different questions.
	since    *seen.Set
	sinceSHA string
	rounds   Rounds
	// roundDiffs is the diff each earlier round carried, once read, and
	// roundAsked is the reads still on their way. rereview is how each todo
	// thread stands against the round being compared, by thread index.
	roundDiffs map[string]*diff.Diff
	roundAsked map[string]bool
	rereview   map[int]answer
	// ticks counts the watcher's beats, which is what paces the head check at a
	// different cadence from the reload.
	ticks int
	// wrote is the artifact's stamp as this screen last saw it, which is what
	// tells a write of its own from an agent's.
	wrote   stamp
	submit  Submitter
	send    Sender
	reactor Reactor
	merge   Merger
	cleanup Cleanup
	head    HeadCheck
	// commitReader reads the commits behind the diff once the frame is up.
	commitReader CommitReader
	browser      Opener
	tree         Tree

	screen   screen
	cursor   int
	selected *selection
	offset   int
	// scrollGoal is where the wheel is gliding the view, paid a glideRate tick
	// at a time by glideStep, and gliding is whether that tick is armed. A key
	// or a click ends the glide, since the view now belongs to it.
	scrollGoal int
	gliding    bool
	width      int
	height     int
	// padX and padY are the margin the last resize left around the page, which
	// a click has to know to read its position in the page's coordinates.
	padX int
	padY int

	keys   keyMap
	styles styles
	rich   richStyles
	search search
	files  files

	// drawn is which renderer u v is on, and refined and lexed are what the rich
	// one reads: the runs of each changed line its partner does not carry, and
	// the grammar's reading of each file, lexed the first time it is drawn.
	drawn   look
	refined diff.Refined
	// reindented is every changed line whose partner says the same thing at another indent.
	reindented map[diff.LineRef]bool
	// moves is both ends of every run the change takes from one place and puts in another.
	moves    map[diff.LineRef]diff.Move
	hunkCols map[hunkRef]int
	lexed    map[string]map[diff.LineRef][]highlight.Span

	// pending is the ] or [ waiting for the object that completes it.
	pending rune
	// chordSeq numbers each chord as it opens so a wait fired under one does
	// not answer for the next, and waiting is whether the chord's page is
	// drawn over the bottom of the frame.
	chordSeq int
	waiting  bool
	// last is the motion n repeats and N reverses, and change is what . replays.
	// Only a change that needs no further input is recorded, since replaying an
	// editor blind is not a repeat of anything.
	last   *motion
	change *change
	// route is the file order an A p answer gave, walked by the path motion
	// it registers as last.
	route []string

	status        string
	failed        bool
	posted        bool
	posting       bool
	merged        bool
	merging       bool
	branchDeleted bool
	deleting      bool
	asking        confirmKind
	labeling      Labeling
	labeler       *labelPicker
	roster        Roster
	involved      []string
	recent        []string
	rostering     bool
	rostered      bool
	fixEvent      string
	fixOne        string
	searching     bool
	view          viewMode
	// editing is the in-place editor, nil when nothing is being written.
	editing *editor
	fold    foldLevel
	// prober is what the review asks about its own files, and nil where nothing
	// can answer: no checkout, or no checker installed for what it holds.
	prober Prober
	// trouble is what the checkers found, placed against this diff, and probing
	// is a pass still out. troubled is the last pass's failure, which the
	// trouble list says rather than a footer nobody kept.
	trouble  diag.Placed
	probing  bool
	troubled error

	// advisor is what a lockfile is asked about, advisories is what came back
	// for each package, and advised is how the asking went per lockfile.
	advisor    Advisor
	advisories map[advisory.Package][]advisory.Note
	advised    map[string]asked
	// versions is what each lockfile's registries are asked without asking,
	// cards is what came back per package, and versioned is how the asking
	// went per lockfile.
	versions  Versions
	cards     map[advisory.Package]versions.Card
	versioned map[string]asked
	// showing is the answer a line question left up, nil when nothing is.
	showing *answerMsg
	// picking is a gd or gr waiting for the letter of a name, and peeked is the
	// card its answer drew under the line.
	picking *pick
	peeked  *peekCard
	// whole is the file the view shows while it is viewFile, and jumps is where
	// jumps left from.
	whole *openFile
	jumps jumpList
	// pane is a program running on a pty inside the frame — $EDITOR on the
	// comment's temp file — with paneMsg saying where its buffer lands and
	// paneFile naming that file, empty where the pane's own screen is the
	// result. The child owns the keys while it runs.
	pane      *pane.Pane
	paneFile  string
	paneMsg   editedMsg
	paneTitle string
	// cosmetic is the structural pass over every hunk, nil until it answers,
	// and shape is what the same pass saw of each hunk's symbols.
	cosmetic map[hunkAt]bool
	shape    shape
	// cost is what the same pass rates the change, shown in the title once it
	// has an answer to show.
	cost    rate.Score
	size    rate.Size
	reading bool
	// made is what counts as written by a machine, which the review groups last
	// and folds.
	made generated.Set
	// asDiffed puts the forge's own order back, which is what to press when the
	// gathering guessed wrong about what belongs together.
	asDiffed bool
	// plan is the order the review reads in, built from commits, rules, the
	// prepared review's [plan], and whatever the structural pass found.
	plan      plan.Plan
	commits   []plan.Commit
	noCommits string
	rules     plan.Rules
	// tour walks the plan's stops and chapters, which p turns off and on.
	tour    bool
	outline outline
	// done is which hunks are already read, so a row can recede without hashing
	// its hunk to find out.
	done map[hunkAt]bool
	// folded is what z has put away by hand.
	folded folded
	help   bool
	// helpAt is how far the legend is scrolled, which a short frame needs
	// because the keys that leave it are at the bottom of it.
	helpAt int
	// helpRect is where the legend was last drawn, and lineRows the row each
	// body line drew, which is what a click is read against.
	helpRect overlay.Rect
	lineRows []int
	// helpPath is which page of the legend is open: the keys taken to reach
	// it, empty at the root.
	helpPath []string
	// helpNote is what a bare keypress inside the legend earned: the row the
	// key names said at full length, kept until the page changes or closes.
	helpNote *helpNote
	// about is what the pull request says about itself, aboutOpen is whether
	// the overlay holding it has the frame, and aboutAt is how far that is
	// scrolled. aboutExpand is z R there: the collapsed sections opened, and
	// aboutZ is the z chord waiting for its second key.
	about       threads.About
	aboutOpen   bool
	aboutExpand bool
	aboutZ      bool
	aboutAt     int
	// next is enter on a posted review: the caller opens the next one staged.
	next bool
	// checkout is C, answered by the caller once the screen has closed.
	checkout bool
	// landing is the comment id the screen was opened on, consumed by the first
	// rebuild with rows to search. A conversation opened out of the queue lands
	// on the thread being answered instead of at the top of the diff.
	landing int64
	// verifying is a head check still in flight, named in the title until it
	// answers. A check that lands late marks the moved head rather than
	// holding the diff back: a review out of the cache is the current head's
	// far more often than not, and a blank frame waiting on the forge is a
	// wait paid on every open.
	verifying bool
	// newHead is the head the pull request is on now, set only when it is not
	// the one this review was staged against. The title says so for as long as
	// the screen is open, because everything drawn under it belongs to the
	// older diff.
	newHead string
	// failure is the last submit that did not post, cleared by one that does.
	// The screen leaves through it, so a run that failed to post says so on
	// stdout and in the exit code rather than only in a footer nobody kept.
	failure error
	// j is the usage journal and built is when the screen was made, which is
	// the start a "how long did the open take" measures from.
	j     *journal.J
	built time.Time
}

// Option configures the review screen.
type Option func(*Model)

// WithStore names where this review's caches live, so the rating the screen
// works out is left where the queue can read it.
func WithStore(root string) Option {
	return func(m *Model) { m.store = root }
}

// WithWorkRoot names the working copy the review was taken in, which is what
// F writes a [TODO: marker into. Only a local review names one: a pull
// request's checkout is borrowed, not the review's to write in.
func WithWorkRoot(root string) Option {
	return func(m *Model) { m.workRoot = root }
}

// WithRepoDir names a checkout of the repository, which is what B opens
// gh-repo-dashboard on. Without one the key is dim.
func WithRepoDir(dir string) Option {
	return func(m *Model) { m.repoDir = dir }
}

// WithSeen carries which hunks have already been read, and where to write that
// back. Without it the screen still runs and space says there is nowhere to
// record an answer, rather than pretending to remember one.
func WithSeen(read *seen.Set, path string) Option {
	return func(m *Model) { m.read, m.seenAt = read, path }
}

// About is what the pull request says about itself, which the caller reads back
// so a screen reopened at another head does not lose it.
func (m *Model) About() threads.About { return m.about }

// WithThreads shows the conversations already open on the pull request, which
// `second-look get` cached. Without it the screen shows the diff and the
// prepared review alone, which is what a review staged before threads were
// fetched has.
func WithThreads(ts []threads.Thread) Option {
	return func(m *Model) { m.threads = ts }
}

// WithAnchor lands the cursor on the thread whose first comment carries this
// id, which is how the conversation queue opens the review at the thread being
// answered rather than at the top of the diff. Zero lands nowhere, and a
// thread the cached review does not carry is said to rather than searched for
// forever.
func WithAnchor(id int64) Option {
	return func(m *Model) { m.landing = id }
}

// New builds the review screen for a prepared review and the diff it was
// staged against.
func New(
	ctx context.Context, r *artifact.Review, d *diff.Diff,
	path string, submit Submitter, opts ...Option,
) *Model {
	st := loadStyles()
	marks, reindented, moves := d.Paired()
	m := &Model{
		ctx: ctx, review: r, diff: d, path: path, submit: submit,
		keys: defaultKeyMap(), styles: st, rich: newRichStyles(st), search: newSearch(),
		files: newFiles(),
		width: minWidth, height: startHeight, folded: newFolded(),
		refined: marks, reindented: reindented, moves: moves,
		lexed:  map[string]map[diff.LineRef][]highlight.Span{},
		lit:    map[string][][]highlight.Span{},
		drawn:  opening,
		built:  time.Now(),
		made:   generated.New(nil),
		around: map[hunkAt]int{}, blobs: map[string][]string{},
		roundDiffs: map[string]*diff.Diff{}, roundAsked: map[string]bool{},
		atHead: map[string]headHits{}, inFlight: map[string]string{}, failedAsks: map[string]string{},
	}

	for _, opt := range opts {
		opt(m)
	}

	m.folded.foldResolved(m.threads)
	m.replan()
	m.tour = len(d.Files) <= tourFiles
	m.outline.open = !m.tour && len(m.plan.Chapters) > 1

	return m
}

// Init lays out the first frame at the assumed size, which the terminal
// corrects with a resize before anything is drawn.
func (m *Model) Init() tea.Cmd {
	m.rebuild()

	// The pass shells out to ast-grep, so it runs behind the first frame rather
	// than in front of it: the rating appears when it is ready and t is a
	// redraw by the time anyone presses it.
	m.wrote, _ = stampOf(m.path)

	cmds := []tea.Cmd{m.checkHead(), m.watch(), m.probe(), m.probeAgent(), m.askHead(), m.readCommits()}
	cmds = append(cmds, m.fetchVersions()...)

	if structure.Available() {
		cmds = append(cmds, readStructure(m.diff, m.made))
	}

	return tea.Batch(cmds...)
}

// commitsMsg is what reading the commits behind d answered.
type commitsMsg struct {
	list []plan.Commit
	why  string
	diff *diff.Diff
	err  error
}

// readCommits reads the commits behind the diff, which for a pull request is an
// API call and a blame per commit.
func (m *Model) readCommits() tea.Cmd {
	if m.commitReader == nil {
		return nil
	}

	read, ctx, head, d := m.commitReader, m.ctx, m.review.HeadSHA, m.diff

	return func() tea.Msg {
		list, why, err := read(ctx, head, d)

		return commitsMsg{list: list, why: why, diff: d, err: err}
	}
}

// applyCommits replans from the commits read. An answer about a diff the screen
// has since replaced is dropped, since a restage brings its own.
func (m *Model) applyCommits(msg commitsMsg) {
	if msg.diff != m.diff {
		return
	}

	if msg.err != nil {
		m.say("reading the commits: "+msg.err.Error(), true)

		return
	}

	m.commits, m.noCommits = msg.list, msg.why
	m.replan()
	m.rebuild()
}

// checkHead asks what the head is now, behind the first frame. It is what makes
// opening a staged review cost no network at all: the screen draws from the
// cache and this says afterwards whether the cache still stands.
func (m *Model) checkHead() tea.Cmd {
	if m.head == nil {
		return nil
	}

	m.verifying = true
	ctx, want := m.ctx, m.review.HeadSHA

	return func() tea.Msg {
		sha, err := m.head(ctx)

		return headMsg{sha: sha, want: want, err: err}
	}
}

// short is a commit at the length a person reads.
func short(sha string) string {
	const n = 7
	if len(sha) <= n {
		return sha
	}

	return sha[:n]
}

// applyStructure takes what the parser found: the rating, the symbols each hunk
// touched, and which hunks changed no code.
func (m *Model) applyStructure(msg structureMsg) {
	asked := m.reading
	m.reading = false

	if msg.err != nil {
		// A pass nobody asked for fails quietly: the rating is missing from the
		// title, which is the whole of what it was for.
		if asked {
			m.say("reading the structure: "+msg.err.Error(), true)
		}

		return
	}

	m.cosmetic, m.shape, m.cost, m.size = msg.cosmetic, msg.shape, msg.score, msg.size
	m.keepScore()
	m.replan()

	if asked {
		m.setFold(foldCosmetic)

		return
	}

	// The pass lands behind the first frame, so the rating, the folds, and the
	// reading order take their answers from it the moment it lands rather than
	// on the next keystroke.
	m.rebuild()
}

// headMsg is what the head check answered.
type headMsg struct {
	sha  string
	want string
	err  error
}

// applyHead reports a head that moved and says nothing about one that did not.
// A check that failed is reported once: the network is not the review's
// problem, and the diff on screen is as good as it was before the question.
func (m *Model) applyHead(msg headMsg) {
	m.verifying = false
	m.j.Since("review", "head", m.built)

	switch {
	case msg.err != nil:
		m.say("could not check the head: "+msg.err.Error(), true)
	case msg.sha == "" || msg.sha == msg.want:
		return
	default:
		m.newHead = msg.sha
		m.say("the head moved to "+short(msg.sha)+"; this diff is the older one, second-look get "+
			strconv.Itoa(m.review.Number)+" restages it", true)
	}
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
	// fresh is where a comment written from nothing lands, nil for every edit
	// of something already staged.
	fresh *staging
	// suggests marks a buffer whose text replaces the line rather than
	// describing it, so what comes back is fenced rather than posted as prose.
	suggests bool
	// transcript marks a pane whose file is what the child printed rather
	// than what it was asked to edit, so the text is cleaned and appended
	// instead of replacing the body.
	transcript bool
	// viewing marks a pane that was only showing a file: exiting reads
	// nothing back and edits nothing.
	viewing bool
}

type sentMsg struct {
	id      string
	summary string
	err     error
	handErr error
}

type submittedMsg struct {
	summary string
	err     error
	// handErr is a hand-off that failed after the review posted.
	handErr error
}

// mergedMsg is what the merge answered with.
type mergedMsg struct {
	summary string
	err     error
}

// branchDeletedMsg is what the local branch cleanup answered with.
type branchDeletedMsg struct {
	summary string
	err     error
}

// Update routes one message. What a command answered with is separated from
// what the keyboard sent, so neither switch grows into the other.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.padX, m.padY = pagePad(msg.Width, msg.Height)
		m.width, m.height = msg.Width-pageSides*m.padX, msg.Height-m.padY

		if m.pane != nil {
			m.pane.Resize(m.width, m.paneHeight())
		}

		m.rebuild()

		return m, nil
	case tea.ColorProfileMsg:
		if msg.Profile <= colorprofile.ASCII {
			m.styles.monochrome()
		}

		return m, nil
	case tea.PasteMsg:
		var cmd tea.Cmd

		switch {
		case m.pane != nil:
			m.pane.Paste(msg.Content)
		case m.agentOpen && m.agent != nil:
			m.agentIn, cmd = m.agentIn.Update(msg)
		}

		return m, cmd
	case tea.MouseWheelMsg:
		cmd := m.wheel(msg)

		return m, cmd
	case scrollMsg:
		cmd := m.glideStep()

		return m, cmd
	case tea.MouseClickMsg:
		m.gliding = false
		m.click(msg.Mouse())

		return m, nil
	case tea.KeyPressMsg:
		m.gliding = false
		was := m.pending
		mod, cmd := m.handleKey(msg)
		m.settlePeek()

		// A chord that just opened arms the wait that draws its page, the way
		// which-key surfaces once a prefix sits a moment.
		if m.pending != 0 && m.pending != was {
			m.chordSeq++
			m.waiting = false

			seq := m.chordSeq
			cmd = tea.Batch(cmd, tea.Tick(chordWait, func(time.Time) tea.Msg {
				return chordWaitMsg{seq}
			}))
		}

		return mod, cmd
	}

	cmd := m.answered(msg)

	return m, cmd
}

// chordWait is the pause before a waiting chord draws its page: long enough
// that a chord typed through never shows one.
const chordWait = 500 * time.Millisecond

// chordWaitMsg is the wait a chord armed when it opened, answered only while
// the same chord is still open.
type chordWaitMsg struct{ seq int }

// answered takes what a command landed with. Every case is a redraw unless it
// says otherwise, and a message nothing here knows changes nothing.
func (m *Model) answered(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case chordWaitMsg:
		m.waiting = m.pending != 0 && msg.seq == m.chordSeq

		return nil
	case sentMsg:
		m.applySent(msg)

		return tea.ClearScreen
	case restagedMsg:
		return m.applyRestaged(msg)
	case commitsMsg:
		m.applyCommits(msg)

		return nil
	case reloadMsg:
		return m.reloaded(msg)
	case mergedMsg:
		m.applyMerge(msg)

		return tea.ClearScreen
	case submittedMsg:
		m.applySubmit(msg)

		// The frame after a post differs from the one before it on the footer
		// alone, and bubbletea v2.0.8 blanks the run of cells a redrawn line
		// shares with the line it replaces ("posting…" then "posted to …"
		// renders as "    ed to …"). Drop the repaint once that is fixed
		// upstream; until then this is the one line that must be readable.
		return tea.ClearScreen
	case answerMsg:
		return m.applyAnswer(msg)
	case peekedMsg:
		return m.applyPeeked(msg)
	case callsMsg:
		return m.applyCalls(msg)
	case fileMsg:
		return m.openWhole(msg)
	}

	if cmd, mine := m.childAnswered(msg); mine {
		return cmd
	}

	if m.absorbForge(msg) {
		return nil
	}

	m.absorb(msg)

	return nil
}

// childAnswered is the share of answered a tenant inside the frame owns: the
// pane's output and exit, and the agent session's lifecycle. It is its own
// switch because a case per message here and one per kind of data in absorb
// is already two switches' worth.
func (m *Model) childAnswered(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pane.WakeMsg:
		// New output on the pane's screen: re-arm the watch that sends the
		// next one.
		if m.pane != nil {
			return m.pane.Watch, true
		}
	case pane.GoneMsg:
		return m.paneGone(msg.Err), true
	case handoff.DoneMsg:
		return m.dashboardDone(msg), true
	case agentUpMsg:
		return m.agentUp(msg), true
	case agentWakeMsg:
		if m.agent != nil && m.agentDead == nil {
			return watchAgent(m.agent), true
		}
	default:
		return nil, false
	}

	return nil, true
}

// absorb takes the messages that only add data, which is every answered()
// case that hands nothing back to run.
func (m *Model) absorb(msg tea.Msg) {
	if m.absorbAgent(msg) {
		return
	}

	switch msg := msg.(type) {
	case editedMsg:
		m.applyEdit(msg)
	case dispatchedMsg:
		m.dispatched(msg)
	case blobMsg:
		m.absorbBlob(msg)
	case structureMsg:
		m.applyStructure(msg)
	case notesMsg:
		m.applyNotes(msg)
	case advisedMsg:
		m.applyAdvisories(msg)
	case askedMsg:
		m.applyAsked(msg)
	case versionedMsg:
		m.applyVersions(msg)
	case headMsg:
		m.applyHead(msg)
	case headedMsg:
		m.applyHeaded(msg)
	case askAnsweredMsg:
		m.applyAnswered(msg)
	case roundMsg:
		m.applyRound(msg)
	case branchDeletedMsg:
		m.applyBranchDeleted(msg)
	}
}

func (m *Model) absorbForge(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case rosterMsg:
		m.applyRoster(msg)
	case labelsReadMsg:
		m.labeler.hear(msg)
	case labelSetMsg:
		m.applyLabelSet(msg)
	default:
		return false
	}

	return true
}

// absorbAgent is absorb's share of a session's own messages.
func (m *Model) absorbAgent(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case agentStateMsg:
		m.agentState = msg.state
	case agentGoneMsg:
		m.agentGone(msg.err)
	case agentTurnMsg:
		m.agentTurned(msg)
	default:
		return false
	}

	return true
}

// motion is a direction and what to stop on, kept so n can repeat it. A
// motion with a steer does not scan: it goes where the steer says, which is
// how a path order that owes the row order nothing is still walked.
type motion struct {
	step  int
	what  string
	want  func(row) bool
	steer func(direction int)
}

// paneKey is the keyboard while a pane runs. The child is a modal program of
// its own, so every key is its except the one that ends it — taking keys back
// piecemeal is how a half-forwarded esc sequence would strand it.
func (m *Model) paneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.EndPane) {
		m.pane.Stop()

		return m, nil
	}

	m.pane.Send(msg)

	return m, nil
}

// keyOwner is whichever surface holds the keyboard right now, if one does:
// a running pane or the open agent session, each modal like a confirmation.
func (m *Model) keyOwner() (func(tea.KeyPressMsg) (tea.Model, tea.Cmd), bool) {
	switch {
	case m.pane != nil:
		return m.paneKey, true
	case m.agentOpen && m.agent != nil:
		return m.agentKey, true
	default:
		return nil, false
	}
}

// writing reports whether the keyboard belongs to text rather than to
// commands — the in-place editor, the search, or a child's own program — which
// is what the journal must never record.
func (m *Model) writing() bool {
	return m.editing != nil || m.labeler != nil || m.searching || m.files.typing || m.pane != nil ||
		m.drafting != nil || (m.agentOpen && m.agent != nil)
}

// promptKey routes a key while something owns the keyboard, reporting whether
// one did.
func (m *Model) promptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case m.editing != nil:
		mod, cmd := m.typeBody(msg)

		return mod, cmd, true
	case m.asking != askNothing:
		mod, cmd := m.answer(msg)

		return mod, cmd, true
	case m.labeler != nil:
		mod, cmd := m.labelKey(msg)

		return mod, cmd, true
	case m.drafting != nil:
		mod, cmd := m.typeQuestion(msg)

		return mod, cmd, true
	case m.searching:
		mod, cmd := m.typing(msg)

		return mod, cmd, true
	case m.files.typing:
		mod, cmd := m.typeFiles(msg)

		return mod, cmd, true
	case m.pending != 0:
		mod, cmd := m.complete(msg)

		return mod, cmd, true
	case m.picking != nil:
		mod, cmd := m.pickName(msg)

		return mod, cmd, true
	}

	return nil, nil, false
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if owner, ok := m.keyOwner(); ok {
		return owner(msg)
	}

	// A prompt, a confirmation, and a half-typed motion each own the keyboard
	// until they are finished, so they are answered before anything else.
	if mod, cmd, ok := m.promptKey(msg); ok {
		return mod, cmd
	}

	// The legend runs longer than a short frame, and the keys that leave it are
	// at the bottom, so it scrolls rather than dropping its tail.
	if m.help {
		return m.readHelp(msg)
	}

	if m.aboutOpen {
		return m.readAbout(msg)
	}

	if m.outline.open {
		return m.readOutline(msg)
	}

	// The hover answer changes nothing, so any key leaves it.
	if m.showing != nil {
		m.showing = nil

		return m, nil
	}

	if cmd, ok := m.judgeKey(msg); ok {
		return m, cmd
	}

	if cmd, ok := m.graphKey(msg); ok {
		return m, cmd
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

		return m.redo(*m.change)
	}

	if m.records(msg) {
		m.remember(0, msg)
	}

	return m.act(msg)
}

// change is what the repeat key does again: a key on its own, or the chord
// prefix and the key that completed it.
type change struct {
	prefix rune
	key    tea.KeyPressMsg
}

func (m *Model) remember(prefix rune, msg tea.KeyPressMsg) {
	m.change = &change{prefix: prefix, key: msg}
}

// redo replays a change where the cursor is standing now. Walking a run of
// comments is therefore ]c m r, then ]c and a dot for each one after it.
func (m *Model) redo(c change) (tea.Model, tea.Cmd) {
	if c.prefix == 0 {
		return m.act(c.key)
	}

	m.pending = c.prefix

	return m.complete(c.key)
}

// wheel moves whichever overlay is up, and starts a glide when none is. A
// notch moves the goal rather than the view, so a trackpad flick is a burst
// of notches that lands as one ease instead of a teleport apiece.
func (m *Model) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	step := wheelStep

	switch msg.Button {
	case tea.MouseWheelUp:
		step = -step
	case tea.MouseWheelDown:
	default:
		return nil
	}

	switch {
	case m.help:
		m.helpAt = max(0, m.helpAt+step)
	case m.aboutOpen:
		m.aboutAt = clamp(m.aboutAt+step, len(m.aboutText(m.width))-m.viewHeight())
	case m.outline.open:
		m.outlineStep(step / wheelStep)
	case m.quiet():
		goal := m.offset
		if m.gliding {
			goal = m.scrollGoal
		}
		m.scrollGoal = clamp(goal+step, m.maxOffset())

		return m.glide()
	}

	return nil
}

// glideRate is how often a wheel glide moves the view, and glideEase the
// share of the way left one move covers: fast when the flick was long, easing
// to a stop a line at a time.
const (
	glideRate = 16 * time.Millisecond
	glideEase = 4
)

// scrollMsg is the glide's tick.
type scrollMsg struct{}

// glide arms the tick that pays a scrollGoal down, unless one is armed.
func (m *Model) glide() tea.Cmd {
	if m.gliding {
		return nil
	}

	m.gliding = true

	return tea.Tick(glideRate, func(time.Time) tea.Msg { return scrollMsg{} })
}

// glideStep moves the view a share of the way to scrollGoal and drags the
// cursor along where the screen would leave it behind, the way a wheel works
// in an editor. It answers the next tick, or none once the goal is reached.
func (m *Model) glideStep() tea.Cmd {
	if !m.gliding {
		return nil
	}

	m.scrollGoal = clamp(m.scrollGoal, m.maxOffset())
	d := m.scrollGoal - m.offset

	if d == 0 {
		m.gliding = false

		return nil
	}

	step := d / glideEase
	if step == 0 && d > 0 {
		step = 1
	} else if step == 0 {
		step = -1
	}
	m.offset += step

	for m.cursor < m.offset {
		m.moveBy(1)
	}

	for m.cursor >= m.offset+m.viewHeight() {
		m.moveBy(-1)
	}

	return tea.Tick(glideRate, func(time.Time) tea.Msg { return scrollMsg{} })
}

// quiet is no overlay, prompt, editor, or pane holding the screen, which is
// when the mouse may move the cursor.
func (m *Model) quiet() bool {
	return !m.help && !m.aboutOpen && !m.outline.open && m.showing == nil &&
		m.labeler == nil && m.editing == nil && m.pane == nil && !m.agentOpen
}

// click closes the legend from outside it, and puts the cursor on the row
// clicked on, read against the lines rowLines last drew.
func (m *Model) click(at tea.Mouse) {
	at = inset(at, m.padX, m.padY)

	if m.help {
		if !m.helpRect.Contains(at.X, at.Y) {
			m.help = false
		}

		return
	}

	if at.Button != tea.MouseLeft || !m.quiet() || (m.tourWide() && at.X <= railCols) {
		return
	}

	y := at.Y - headerRows
	if y < 0 || y >= len(m.lineRows) {
		return
	}

	i := m.lineRows[y]
	if i < 0 || i >= len(m.screen.rows) || passed(m.screen.rows[i]) {
		return
	}

	m.cursor = i
	m.say("", false)
	m.follow()
}

// readHelp walks the legend's pages and closes it, and swallows everything
// else, so a key pressed while reading it does not act on the review behind
// it. A key that opens a page walks into it the way the chord itself would,
// and a key that opens nothing is answered rather than run: its note is what
// the key does, said at the length the row abbreviates. The scroll keys are
// the arrows, the page keys, and the wheel, because j and k are answers of
// their own inside the legend.
func (m *Model) readHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	const halfPage = 2

	if _, ok := keyhint.At(m.helpTree(), append(m.helpPath, msg.String())); ok {
		m.helpPath = append(m.helpPath, msg.String())
		m.helpAt, m.helpNote = 0, nil

		return m, nil
	}

	switch msg.String() {
	case escKey:
		if len(m.helpPath) > 0 {
			m.helpPath = m.helpPath[:len(m.helpPath)-1]
		} else {
			m.help = false
		}
		m.helpAt, m.helpNote = 0, nil
	case "?", "ctrl+c":
		m.help, m.helpAt, m.helpPath, m.helpNote = false, 0, nil, nil
	case "up":
		m.helpAt--
	case "down":
		m.helpAt++
	case "pgup", "ctrl+u":
		m.helpAt -= m.viewHeight() / halfPage
	case "pgdown", "ctrl+d":
		m.helpAt += m.viewHeight() / halfPage
	case "home":
		m.helpAt = 0
	case "end":
		m.helpAt = pastEnd
	default:
		m.helpNote = m.helpNoteFor(msg.String())
	}

	m.helpAt = max(0, m.helpAt)

	return m, nil
}

// helpNote is the legend's answer for one key: its group, its row, and the
// prose the group carries under it.
type helpNote struct {
	group string
	key   string
	what  string
	prose []string
}

// helpNoteFor answers the key's note on the open page, or nil where the key
// names no leaf. A compound row like j/k answers for either half of it, and
// the space bar answers for "space".
func (m *Model) helpNoteFor(k string) *helpNote {
	page, _ := keyhint.At(m.helpTree(), m.helpPath)

	for i, h := range page {
		if h.Head || h.Key == "" || len(h.Kids) > 0 || !keySays(h.Key, k) {
			continue
		}

		note := &helpNote{key: h.Key, what: h.What}
		lo := 0
		for j := i - 1; j >= 0; j-- {
			if page[j].Head {
				note.group, lo = page[j].What, j

				break
			}
		}
		for j := lo; j < len(page) && (j <= i || !page[j].Head); j++ {
			if p := page[j]; p.Key == "" && !p.Head {
				note.prose = append(note.prose, p.What)
			}
		}

		return note
	}

	return nil
}

// keySays reports whether the key k is inside the row's label, so j answers
// for j/k, ctrl+d for ctrl+d/u, and the space bar for "space".
func keySays(label, k string) bool {
	for _, part := range strings.Split(label, "/") {
		if part == k {
			return true
		}
	}

	return label == spaceKey && (k == " " || k == spaceKey)
}

// asks handles the keys that act on the row under the cursor: the ones that go
// out to the parser or the forge and swap the answer in once it lands, and the
// ones that open a range or an editor on it. It reports whether one matched.
func (m *Model) asks(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Structure):
		return m.askStructure(), true
	case key.Matches(msg, m.keys.Restage):
		return m.askRestage(), true
	case key.Matches(msg, m.keys.Dashboard):
		return m.openDashboard(), true
	case key.Matches(msg, m.keys.Round):
		return m.askRound(), true
	case key.Matches(msg, m.keys.Suggest):
		return m.suggest(), true
	case key.Matches(msg, m.keys.Label):
		return m.openLabels(), true
	case key.Matches(msg, m.keys.Range):
		m.askRange()

		return nil, true
	case key.Matches(msg, m.keys.Accept) && m.screen.rows[m.cursor].kind == rowMoved:
		m.otherEnd()

		return nil, true
	case key.Matches(msg, m.keys.Accept) && m.screen.rows[m.cursor].kind == rowPeek:
		return m.openSite(m.screen.rows[m.cursor].site), true
	case key.Matches(msg, m.keys.Accept) && m.screen.rows[m.cursor].site.Path != "":
		return m.openSite(m.screen.rows[m.cursor].site), true
	}

	return nil, false
}

// overlayKey answers the two keys that put something over the diff: the legend
// and what the pull request says about itself. Neither changes the review.
func (m *Model) overlayKey(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.Help):
		m.help = !m.help
		m.helpAt, m.helpPath, m.helpNote = 0, nil, nil
	case key.Matches(msg, m.keys.About):
		m.aboutOpen, m.aboutAt = true, 0
	default:
		return false
	}

	return true
}

// leaves answers the two keys that leave the screen: q, which closes the legend
// and then a peek first where they are open and otherwise hands back to whatever
// is behind the review, and enter on a review that has posted, which carries the sitting on
// to the next one staged rather than ending on each review.
func (m *Model) leaves(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.help {
			m.help, m.helpAt, m.helpPath, m.helpNote = false, 0, nil, nil

			return nil, true
		}

		if m.view == viewFile {
			m.leaveFile()

			return nil, true
		}

		if m.peeked != nil {
			m.closePeek()

			return nil, true
		}

		esc := msg.String() == escKey

		return func() tea.Msg { return leftMsg{esc: esc} }, true
	case m.posted && key.Matches(msg, m.keys.Accept):
		m.next = true

		return tea.Quit, true
	}

	return nil, false
}

// mode handles the keys that change what the screen is showing rather than what
// the review says, and reports whether one of them matched.
func (m *Model) mode(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	if cmd, ok := m.asks(msg); ok {
		return true, m, cmd
	}

	if cmd, ok := m.leaves(msg); ok {
		return true, m, cmd
	}

	switch {
	case m.overlayKey(msg):
	case key.Matches(msg, m.keys.More), key.Matches(msg, m.keys.Less):
		by := step
		if key.Matches(msg, m.keys.Less) {
			by = -step
		}

		cmd := m.grow(by)

		return true, m, cmd
	case m.reshapes(msg):
		return true, m, nil
	case key.Matches(msg, m.keys.Ask) && m.selected != nil:
		cmd := m.beginQuestion()

		return true, m, cmd
	case m.startsChord(msg):
		return true, m, nil
	case key.Matches(msg, m.keys.Search):
		cmd := m.begin()

		return true, m, cmd
	case key.Matches(msg, m.keys.Files):
		cmd := m.beginFiles()

		return true, m, cmd
	default:
		return false, m, nil
	}

	return true, m, nil
}

// startsChord opens the pending-key state for every chord the screen has,
// saying what each accepts while it waits. The g chord lives here rather than
// in keys.Top because the queue and the help still read a bare g as the top.
func (m *Model) startsChord(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.Look):
		m.pending = 'u'

		m.say(m.chord("u", lookObjects()), false)
	case msg.String() == "g":
		m.pending = 'g'

		m.say(m.chord("g", goObjects()), false)
	case key.Matches(msg, m.keys.Zed):
		m.pending = 'z'

		m.say(m.chord("z", foldObjects()), false)
	case key.Matches(msg, m.keys.State):
		m.pending = 'm'

		m.say(m.chord("m", states()), false)
	case key.Matches(msg, m.keys.React):
		m.pending = ','

		m.say(m.chord(",", reactObjects()), false)
	case key.Matches(msg, m.keys.Ask):
		m.pending = 'A'
		m.say(m.chord("A", m.askObjects()), false)
	case key.Matches(msg, m.keys.Forward), key.Matches(msg, m.keys.Backward):
		m.pending = ']'
		if key.Matches(msg, m.keys.Backward) {
			m.pending = '['
		}

		m.say(m.chord(string(m.pending), objects(string(m.pending))), false)
	default:
		return false
	}

	return true
}

// records marks the unchorded changes the repeat key can replay: the ones that
// take no further input, so replaying one does exactly what it did the first
// time. A chord records itself once its second key lands.
func (m *Model) records(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Seen)
}

// complete answers the second key of a chord. An unknown letter cancels rather
// than waiting, since a half-typed chord that swallows the next keystroke is
// worse than one that says it did not land.
func (m *Model) complete(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	prefix := m.pending
	m.pending, m.waiting = 0, false

	if key.Matches(msg, m.keys.Quit) {
		m.say("", false)

		return m, nil
	}

	switch prefix {
	case 'z':
		cmd := m.zedKey(prefix, msg)

		return m, cmd
	case 'a':
		cmd := m.writeAs(msg)

		return m, cmd
	case 'S':
		return m.submitAs(msg)
	case 'u':
		m.toggleLook(msg)

		return m, nil
	case 'g':
		return m.gotoKey(msg)
	case 'H':
		cmd := m.sinceRound(msg.String())

		return m, cmd
	case 'A':
		return m.adviseKey(msg)
	case ',':
		m.reactTo(msg)

		return m, nil
	case 'm':
		if !m.stateKey(msg) {
			m.say("no state for "+msg.String()+"; r ready, d draft, t todo, x skip", false)

			return m, nil
		}

		m.remember(prefix, msg)

		return m.act(msg)
	}

	m.object(prefix, msg)

	return m, nil
}

// zedKey answers the z chord: za expands the call sites of a graph's selected
// node, and otherwise z places the frame or folds a note.
func (m *Model) zedKey(prefix rune, msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case msg.String() == "a" && m.graphHere() != nil:
		return m.expandSites()
	case m.frameAt(msg):
	case m.foldNote(msg):
		m.remember(prefix, msg)
	}

	return nil
}

// gotoKey answers the g chord: gg is the top it always was, d and r ask the
// language server where each name on the line is declared and read, and c draws
// the call graph of the function the line is in.
func (m *Model) gotoKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "g":
		m.cursor = 0
		m.say("", false)
		m.follow()
	case "d":
		cmd := m.lookup(diag.Definitions)

		return m, cmd
	case "r":
		cmd := m.lookup(diag.References)

		return m, cmd
	case "c":
		cmd := m.calls()

		return m, cmd
	case "f":
		cmd := m.wholeFile()

		return m, cmd
	default:
		m.say("no place for "+msg.String()+"; g top, d defined, r used, c calls, f file", false)
	}

	return m, nil
}

// fileMsg is the whole file read for the pane, or the reason it could not be.
type fileMsg struct {
	path  string
	at    int
	lines []string
	err   error
}

// wholeFile answers gf: the file the cursor is standing on, all of it, opened
// in $EDITOR at the line the row carries. The file is the blob the review
// reads — the commit's copy rather than the working tree's — so the pane can
// open on a review with no checkout at all.
func (m *Model) wholeFile() tea.Cmd {
	path, at := m.siteUnderCursor()
	if path == "" {
		m.say("no file under the cursor", false)

		return nil
	}

	if m.blob == nil {
		m.say("no way to read "+path+" from here", false)

		return nil
	}

	m.say("reading "+path+"…", false)

	ctx, read := m.ctx, m.blob

	return func() tea.Msg {
		lines, err := readSite(ctx, read, path)

		return fileMsg{path: path, at: at, lines: lines, err: err}
	}
}

// siteUnderCursor is the file and line the cursor row stands for: a peek row's
// own place, and otherwise the line of the diff.
func (m *Model) siteUnderCursor() (string, int) {
	r := m.screen.rows[m.cursor]
	if r.kind == rowPeek {
		return r.site.Path, r.site.Line
	}

	return r.path, r.line.New
}

// openWhole lands the file's lines as a temp file the editor opens. The temp
// name keeps the real file's name, since that is what filetype detection
// reads; what the pane holds is a viewing session, so exiting reads nothing
// back.
func (m *Model) openWhole(msg fileMsg) tea.Cmd {
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return nil
	}

	file, err := os.CreateTemp("", "*-"+filepath.Base(msg.path))
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	name := file.Name()

	if _, err := file.WriteString(strings.Join(msg.lines, "\n") + "\n"); err != nil {
		m.say(err.Error(), true)
		//nolint:gosec,errcheck // a temp file that outlives the failure is not worth an error path
		os.Remove(name)

		return nil
	}

	if err := file.Close(); err != nil {
		m.say(err.Error(), true)
		//nolint:gosec,errcheck // a temp file that outlives the failure is not worth an error path
		os.Remove(name)

		return nil
	}

	p, err := pane.New(m.ctx, editorLineArgv(name, msg.at), m.width, m.paneHeight())
	if err != nil {
		//nolint:gosec,errcheck // a temp file that outlives the pane is not worth an error path
		os.Remove(name)
		m.say(err.Error(), true)

		return nil
	}

	return m.seat(p, name, editedMsg{viewing: true}, filepath.Base(msg.path))
}

// write answers the three keys that put prose into the review: the comment's
// note, whatever the cursor is standing on, and a comment that does not exist
// yet.
func (m *Model) write(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Note):
		return m.editNote()
	case key.Matches(msg, m.keys.Write):
		m.askWrite()

		return nil
	}

	return m.edit()
}

// stateKey reports whether a key names one of the three comment states.
func (m *Model) stateKey(msg tea.KeyPressMsg) bool {
	_, ok := m.stateOf(msg)

	return ok || key.Matches(msg, m.keys.Resolve)
}

// stateOf is the status a key names, and false for a key that names none.
func (m *Model) stateOf(msg tea.KeyPressMsg) (string, bool) {
	switch {
	case key.Matches(msg, m.keys.Ready):
		return artifact.StatusReady, true
	case key.Matches(msg, m.keys.Draft):
		return artifact.StatusDraft, true
	case key.Matches(msg, m.keys.Skip):
		return artifact.StatusSkip, true
	case key.Matches(msg, m.keys.Todo):
		return artifact.StatusTodo, true
	}

	return "", false
}

// foldNote answers the z chord: za inverts what the cursor is standing on, zo
// and zc name a direction, zR and zM answer for the whole review, and zi
// inverts the whole review the way za inverts one block. It reports whether the
// key named a fold, which is what the repeat key records.
func (m *Model) foldNote(msg tea.KeyPressMsg) bool {
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
	case "i":
		m.foldAll(m.anyFolded())
	default:
		m.say("no fold for "+msg.String()+"; a this one, i invert, R open all, M close all", false)

		return false
	}

	return true
}

// anyFolded reports whether the frame is holding anything back, which is what
// zi inverts. It reads the rows rather than the maps, so the answer is about
// what the eye is looking at rather than about a fold on a file the current
// view does not draw.
func (m *Model) anyFolded() bool {
	for i := range m.screen.rows {
		if m.screen.rows[i].folded {
			return true
		}
	}

	return false
}

// foldHere folds what the cursor is on, which is the file from its name, the
// hunk from anywhere inside it, the run of removed lines from any of them, and
// the note from the comment it belongs to.
func (m *Model) foldHere(open bool) {
	r := m.screen.rows[m.cursor]

	switch {
	case r.kind == rowTurn:
		m.folded.turns[r.comment] = open
	case r.kind == rowThread && r.head:
		m.folded.threads[r.thread] = !open
	case r.kind == rowThread, isMember(r):
		if !m.foldInside(r, open) {
			return
		}
	case r.skips.path != "":
		m.folded.skips[r.skips] = open
	case r.kind == rowGroup:
		if !m.foldChapter(r, open) {
			return
		}
	case r.kind == rowFile && r.path != "":
		m.openFile(r.path, open)
	case r.gone > 0:
		m.folded.gone[goneAt{r.path, r.gone}] = open
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

// foldChapter makes a chapter draw its hunks or its summary. A shape's summary
// is its exact members as rows, and anything else's is its folded files.
func (m *Model) foldChapter(r row, open bool) bool {
	i := slices.IndexFunc(m.screen.groups, func(g planGroup) bool { return g.stop == 0 && g.name == r.path })
	if i < 0 {
		return false
	}

	g := m.screen.groups[i]

	if p := m.patternOf(g); p != nil {
		for _, mem := range p.Members {
			if mem.Exact() {
				m.folded.members[mem.Path] = open
			}
		}

		return true
	}

	for _, p := range g.parts {
		m.openFile(p.path, open)
	}

	return true
}

// fileLayout is the part of the layout that decides whether a file is shut.
func (m *Model) fileLayout() layout {
	lay := layout{fold: m.folded, made: m.made}
	if !m.asDiffed {
		lay.folds = foldedPaths(m.plan.Chapters)
	}

	return lay
}

// openFile draws a file's hunks or its summary. What a machine wrote starts as
// the summary and everything else as the hunks, so the stored bit is whether
// the file is away from where it started.
func (m *Model) openFile(path string, open bool) {
	m.folded.files[path] = open == m.fileLayout().shutByDefault(path)
}

func (m *Model) patternOf(g planGroup) *plan.Pattern {
	return layout{plan: m.plan.Chapters}.patternOf(g)
}

// foldInside folds a block of a thread or a shape's file, and reports whether
// there was one to fold.
func (m *Model) foldInside(r row, open bool) bool {
	if isMember(r) {
		if !m.exactMember(r.path) {
			m.say(r.path+" is drawn in full beyond the shape", false)

			return false
		}

		m.folded.members[r.path] = open

		return true
	}

	at, ok := foldKey(r)
	if !ok {
		m.say("nothing to fold here", false)

		return false
	}

	m.folded.blocks[at] = open
	// Opening anything inside a capped thread asks for more of it than the cap draws.
	if open {
		m.folded.blocks[blockAt{noteAt{r.thread, 0}, capBlock}] = true
	}

	return true
}

// openHere is whether what the cursor is on is showing, which is what za has
// to invert.
func (m *Model) openHere() bool {
	r := m.screen.rows[m.cursor]

	switch {
	case r.kind == rowTurn:
		return m.folded.turns[r.comment]
	case r.kind == rowThread && r.head:
		return !m.folded.threads[r.thread]
	case r.kind == rowThread:
		at, ok := foldKey(r)

		return !ok || m.folded.blocks[at]
	case isMember(r):
		return m.folded.members[r.path]
	case r.skips.path != "":
		return m.folded.skips[r.skips]
	case r.kind == rowGroup:
		return !r.folded
	case r.kind == rowFile:
		return !m.fileLayout().shut(r.path)
	case r.gone > 0:
		return m.folded.gone[goneAt{r.path, r.gone}]
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

	if open {
		for _, at := range goneRuns(m.diff) {
			m.folded.gone[at] = true
		}

		for at, blocks := range m.notes {
			for i := tailBlock; i < foldables(blocks); i++ {
				m.folded.blocks[blockAt{at, i}] = true
			}
		}
	}

	for i := range m.diff.Files {
		m.openFile(filePath(&m.diff.Files[i]), open)
	}

	if !open {
		for i := range m.threads {
			m.folded.threads[i] = true
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
	for i := range m.screen.rows {
		if m.screen.rows[i].comment == index && m.screen.rows[i].folded {
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
		m.repeatable(motion{step: step, what: hunkNoun, want: isHunk})
	case "f":
		m.repeatable(motion{step: step, what: fileNoun, want: isKind(rowFile)})
	case "]", "[":
		m.repeatable(chapterMotion(step))
	case "s":
		m.repeatable(stopMotion(step))
	case "c":
		m.repeatable(motion{step: step, what: commentWord, want: isComment})
	case "t":
		m.repeatable(motion{step: step, what: "thread", want: isThread})
	case "u":
		m.repeatable(motion{step: step, what: "unread hunk", want: m.isUnread})
	case "p":
		m.repeatable(motion{step: step, what: "problem", want: isTrouble})
	case "a":
		m.repeatable(motion{step: step, what: "ask", want: isAsk})
	default:
		m.say("no motion for "+msg.String(), false)
	}
}

// repeatable runs a motion and remembers it, so n walks the same object.
func (m *Model) repeatable(mo motion) {
	m.last = &mo
	m.runMotion(mo, 1)
}

// runMotion applies a remembered motion in a direction: a steered motion is
// driven rather than scanned for.
func (m *Model) runMotion(mo motion, direction int) {
	from := m.snapshot()

	if mo.steer != nil {
		mo.steer(direction * mo.step)
	} else {
		m.jump(mo.step*direction, mo.what, mo.want)
	}

	m.jumpedFrom(from)
}

// toNote walks to the next or previous thing wanting a decision. It reports its
// own outcome, so it must return before the clear at the end of moved.
func (m *Model) toNote(step int) bool {
	m.repeatable(motion{step: step, what: "comment or thread", want: isHead})

	return true
}

// chord is what the second key can be, shown while the first is waiting.
func (*Model) chord(prefix string, hints [][2]string) string {
	return prefix + keyhint.Gap + keyhint.Line(keyhint.Styles{}, asHints(hints))
}

// moved handles every key that only changes where the cursor is, so the action
// keys below stay a flat list of things that change the review.
func (m *Model) moved(msg tea.KeyPressMsg) bool {
	const halfPage = 2

	if m.walked(msg) {
		return true
	}

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
		m.cursor = m.lastStop()
	case key.Matches(msg, m.keys.NextNote):
		return m.toNote(1)
	case key.Matches(msg, m.keys.PrevNote):
		return m.toNote(-1)
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

// frameAt puts the cursor's line at the middle, top, or bottom of the frame,
// which is vim's zz, zt, zb. It reports whether the key named one of them.
func (m *Model) frameAt(msg tea.KeyPressMsg) bool {
	const middle = 2

	h := m.viewHeight()

	switch msg.String() {
	case "z":
		m.offset = m.cursor - h/middle
	case "t":
		m.offset = m.cursor
	case "b":
		m.offset = m.cursor - h + 1
	default:
		return false
	}

	m.offset = clamp(m.offset, m.maxOffset())
	m.fitCursor()
	m.say("", false)

	return true
}

// fitCursor walks the offset forward while the cursor's row is out of the
// frame, which wrap makes possible: the bounds the callers set are counted in
// rows, and a wrapped row is several lines tall.
func (m *Model) fitCursor() {
	if !m.drawn.wrap {
		return
	}

	h := m.viewHeight()
	width := bodyWidth(m.diffWidth(), scrollbar(h, len(m.screen.rows), m.offset))

	for m.offset < m.cursor {
		tall := 0

		for i := m.offset; i <= m.cursor; i++ {
			if tall += len(m.rowBodies(m.screen.rows[i], width)); tall > h {
				break
			}
		}

		if tall <= h {
			return
		}

		m.offset++
	}
}

// peek scrolls the frame by a line without moving the cursor. A frame that
// cannot move says so rather than doing nothing, since the same key in a
// diff that already fits looks dead.
func (m *Model) peek(step int) bool {
	at := m.offset
	m.offset = clamp(at+step, m.maxOffset())

	if m.offset == at {
		m.say("nothing further that way", false)

		return true
	}

	m.say("", false)

	return true
}

// showThreads opens the conversations, and a second press leaves them.
//
// It is off the view cycle, so what it goes back to is the diff however it was
// reached.
func (m *Model) showThreads() { m.toggleView(viewThreads) }

// showTrouble opens what the checkers found, and a second press leaves it.
func (m *Model) showTrouble() {
	if m.prober == nil {
		m.say("nothing here can check this change: no checkout, "+
			"no language server installed for it, and no check configured", false)

		return
	}

	m.toggleView(viewTrouble)
}

// toggleView opens one of the views that sit off the cycle, or leaves it for
// the diff.
func (m *Model) toggleView(v viewMode) {
	was, at := m.view, m.here()
	if was == viewDiff {
		m.left = at
	}

	m.view = v
	if was == v {
		m.view, at = viewDiff, m.left
	}

	m.rebuild()
	m.goTo(at)

	if m.view == viewDiff {
		m.say("the diff", false)

		return
	}

	m.say(m.view.String(), false)
}

// cycleView walks the three views, keeping the cursor on the same comment
// across the change. Losing your place is what makes another view a detour
// rather than a shortcut.
func (m *Model) cycleView() { m.goView(m.view.next()) }

// showComments opens what the review will post, and a second press leaves it
// for the diff, the way t leaves the threads.
func (m *Model) showComments() {
	if m.view == viewComments {
		m.goView(viewDiff)

		return
	}

	m.goView(viewComments)
}

func (m *Model) goView(v viewMode) {
	was, at := m.current(), m.here()
	m.view = v
	m.rebuild()

	m.goTo(at)
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
	if m.last == nil && m.touring() {
		m.runMotion(m.tourMotion(), direction)

		return true
	}

	if m.last == nil {
		m.say("no motion to repeat; ] names one", false)

		return true
	}

	m.runMotion(*m.last, direction)

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

	if r.kind == rowFile || len(r.refs) > 0 {
		refs := m.hunksOf(r.path)
		if len(r.refs) > 0 {
			refs = m.seenRefs(r.refs)
		}

		if len(refs) == 0 {
			m.say("nothing to read in "+r.path, false)

			return
		}

		read := !m.allRead(refs)
		ids := make([]seen.ID, 0, len(refs))

		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}

		name := r.path
		if name == "" {
			name = "the exact files"
		}

		m.read.Mark(read, ids...)
		m.saveRead(name + ": " + plural(len(refs), "hunk") + " " + readWord(read))

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

// keepScore leaves the rating where the queue can read it, so ordering eighty
// rows costs no diff reads. A failure is silent: the number is a convenience,
// and a queue with no number for a row is what a row nobody has opened looks
// like.
func (m *Model) keepScore() {
	if !m.cost.Rated() || m.store == "" {
		return
	}

	//nolint:errcheck // the rating is a convenience; nothing depends on it landing
	_ = artifact.SaveScore(m.store, m.review.HeadSHA,
		artifact.Cost{Total: m.cost.Total, Added: m.size.Added, Removed: m.size.Removed})
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

	return readStructure(m.diff, m.made)
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

// fileProgress is how much of one file has been read. A review opened with no
// marks to read has none read rather than none at all.
func (m *Model) fileProgress(path string) (int, int) {
	refs := m.hunksOf(path)
	if m.read == nil {
		return 0, len(refs)
	}

	return m.read.Count(refs), len(refs)
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
	if !isHunk(r) || m.read == nil {
		return false
	}

	return !m.read.Has(seen.Hunk(m.diff, r.path, r.hunk))
}

// isHunk accepts a row standing for one hunk of the diff, which is a hunk
// heading or the heading of a file drawing only that hunk. The review's own
// body and note, and the line naming a rename or a binary payload, share the
// heading style without being hunks, so this looks at the number rather than
// at the kind.
func isHunk(r row) bool {
	return r.hunk > 0 && (r.kind == rowHunk || r.kind == rowFile)
}

func isComment(r row) bool { return r.head && r.kind == rowComment && r.comment >= 0 }

// isThread is an open thread's head, which skips a resolved one the way ]c
// skips a hunk with nothing left to say about it.
func isThread(r row) bool { return r.head && r.kind == rowThread && !r.resolved }

// isTrouble is a note's first row, so ]p walks the notes rather than the lines
// a long one wrapped onto.
func isTrouble(r row) bool { return r.head && r.kind == rowTrouble }

func isKind(k rowKind) func(row) bool {
	return func(r row) bool { return r.kind == k }
}

// isHead is anything tab walks: a staged comment or an open thread, either one
// still wanting a decision. A resolved thread is neither, so it is skipped the
// same way isThread skips it on its own.
func isHead(r row) bool {
	return isComment(r) || isThread(r)
}

func (m *Model) act(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Posting removes the prepared review, and every action below writes it
	// back. One keystroke after a successful post would recreate the file that
	// was deleted to stop `second-look post` from publishing a second copy.
	if m.settled() && m.changes(msg) {
		m.say(m.closedWord(), false)

		return m, nil
	}

	if cmd, ok := m.judgeKey(msg); ok {
		return m, cmd
	}

	if status, ok := m.stateOf(msg); ok {
		m.setStatus(status)

		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Dispatch):
		cmd := m.dispatch()

		return m, cmd
	case key.Matches(msg, m.keys.Hover):
		cmd := m.hover()

		return m, cmd
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
	case key.Matches(msg, m.keys.Marker):
		cmd := m.markTodo()

		return m, cmd
	case key.Matches(msg, m.keys.Note), key.Matches(msg, m.keys.Edit),
		key.Matches(msg, m.keys.Write):
		cmd := m.write(msg)

		return m, cmd
	case key.Matches(msg, m.keys.Open):
		m.browse()
	default:
		m.confirmKey(msg)
	}

	return m, nil
}

// confirmKey answers the keys that ask before they act. Each sends something
// that cannot be taken back (a review, a merge, a branch, a list of package
// names), so none of them does anything but put the question up.
func (m *Model) confirmKey(msg tea.KeyPressMsg) {
	switch {
	case key.Matches(msg, m.keys.Submit):
		m.askSubmit()
	case key.Matches(msg, m.keys.Merge):
		m.askMergeNow()
	case key.Matches(msg, m.keys.DeleteBranch):
		m.askDeleteBranchNow()
	case key.Matches(msg, m.keys.Advisories):
		m.askAdvisoriesNow()
	}
}

// closedWord says why a change was refused, since a merged pull request and a
// posted review are two different endings.
func (m *Model) closedWord() string {
	if m.merged {
		return "already merged; nothing more is staged for this pull request"
	}

	return "already posted; GitHub has this review now"
}

// changes reports whether a key writes to the prepared review.
func (m *Model) changes(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Ready) ||
		key.Matches(msg, m.keys.Draft) ||
		key.Matches(msg, m.keys.Skip) ||
		key.Matches(msg, m.keys.Todo) ||
		key.Matches(msg, m.keys.Resolve) ||
		key.Matches(msg, m.keys.Edit) ||
		key.Matches(msg, m.keys.Write)
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
	c.Resolve = false
	setState(c, status)

	m.save(fmt.Sprintf("%s is %s", c.ID, status))
}

// setState sets a status. Validation requires a reason for a skip, and refusing
// the keystroke over a sentence nobody has written yet would be worse than a
// plain default.
func setState(c *artifact.Comment, status string) {
	c.Status = status

	switch {
	case status != artifact.StatusSkip:
		c.SkipReason = ""
	case c.SkipReason == "":
		c.SkipReason = "declined during review"
	}
}

// reshapes answers the keys that change what the frame shows rather than what
// the review says: the views, the order, and the two filters.
func (m *Model) reshapes(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.View):
		m.cycleView()
	case key.Matches(msg, m.keys.Comments):
		m.showComments()
	case key.Matches(msg, m.keys.Order):
		m.cycleOrder()
	case key.Matches(msg, m.keys.Tour):
		m.toggleTour()
	case key.Matches(msg, m.keys.Outline):
		m.openOutline()
	case key.Matches(msg, m.keys.Fold):
		m.setFold(foldWhitespace)
	case key.Matches(msg, m.keys.OnlyNew):
		m.narrow()
	case key.Matches(msg, m.keys.Threads):
		m.showThreads()
	case key.Matches(msg, m.keys.Trouble):
		m.showTrouble()
	default:
		return false
	}

	return true
}

// narrow answers U: what is left is what this pass has not read, which is the
// second look at a pull request that moved.
func (m *Model) narrow() {
	if m.read == nil {
		m.say("no read marks here, so there is nothing to narrow to", false)

		return
	}

	was := m.here()
	m.onlyNew = !m.onlyNew
	m.rebuild()
	m.goTo(was)
	m.say(newWord(m.onlyNew), false)
}

func (m *Model) save(ok string) {
	if err := artifact.Save(m.path, m.review); err != nil {
		m.say(err.Error(), true)

		return
	}

	// The watcher reads the same file, so a write of this screen's own is
	// recorded here rather than arriving a second later as a change.
	m.wrote, _ = stampOf(m.path)

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
		if m.review.Body == "" && m.review.Note == "" {
			return m.beginEdit("editing the review body", "",
				editedMsg{index: reviewBody, replyTo: -1, field: fieldBody})
		}

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

// C leaves the screen so the working copy can be moved onto the pull request,
// which is the one thing reviewing from the API cannot supply.
func (m *Model) wantCheckout() tea.Cmd {
	if m.tree == TreeOnHead {
		where := "the checkout is already on this pull request"
		if m.review.Local {
			where = "the checkout is the change under review"
		}

		m.say(where, false)

		return nil
	}

	m.checkout = true

	return tea.Quit
}

// noTree is why the shell has nowhere useful to run, which is worth naming
// rather than opening one against whatever the working directory happens to be.
func (m *Model) noTree() string {
	if m.tree == TreeNone {
		return "no checkout of " + m.review.Owner + "/" + m.review.Repo +
			" here; C claims the best clone"
	}

	return "the checkout is on another branch; C moves it onto this pull request"
}

// shell runs $SHELL in the pane and appends what the session printed to the
// note under the cursor. Running the code under review and then writing the
// comment is the flow this exists for, and a transcript is what makes the
// comment evidence rather than a claim.
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

	shell := shellrun.Shell()

	p, err := pane.New(m.ctx, shellrun.Argv(), m.width, m.paneHeight())
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	return m.seat(p, "", editedMsg{
		index: i, replyTo: -1, field: fieldNote, transcript: true,
	}, filepath.Base(shell))
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

// open puts start in a temporary file and opens $EDITOR on it in the pane, so
// the diff stays on screen while the buffer is being written. The child
// exiting lands what came back shaped as msg.
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

	argv := editorArgv(name)

	p, err := pane.New(m.ctx, argv, m.width, m.paneHeight())
	if err != nil {
		//nolint:gosec,errcheck // a temp file that outlives the edit is not worth an error path
		os.Remove(name)
		m.say(err.Error(), true)

		return nil
	}

	return m.seat(p, name, msg, filepath.Base(argv[0]))
}

// seat installs a started pane as the screen's child: paneFile is the temp
// file its exit leaves worth reading — empty for a shell, whose transcript is
// the screen itself — paneMsg the shape that read takes, and title what the
// divider names it.
func (m *Model) seat(p *pane.Pane, name string, msg editedMsg, title string) tea.Cmd {
	m.pane, m.paneFile, m.paneMsg = p, name, msg
	m.paneTitle = title
	m.j.Event("review", "pane "+title)

	// A hover answer still in flight would draw over the shrunken body while
	// the pane holds the keyboard, so it is dismissed rather than left up.
	m.showing = nil

	return p.Watch
}

// paneGone is the child exiting: the buffer an editor wrote replaces the
// body, the session a shell ran is appended to it as the pane drew it.
func (m *Model) paneGone(err error) tea.Cmd {
	p, name, msg := m.pane, m.paneFile, m.paneMsg
	m.pane, m.paneFile, m.paneTitle = nil, "", ""
	p.Kill()

	if msg.viewing {
		if err != nil {
			m.say(err.Error(), true)
		}
		//nolint:gosec,errcheck // a temp file that outlives the pane is not worth an error path
		os.Remove(name)

		return tea.ClearScreen
	}

	if msg.transcript {
		// A shell that exits non-zero still ran, and what the pane drew is
		// still the evidence it existed to keep.
		text := p.Transcript()
		if err == nil || text != "" {
			msg.body = appendTranscript(m.review.Comments[msg.index].Note, text)
		} else {
			msg.err = err
		}
		m.applyEdit(msg)

		return tea.ClearScreen
	}

	//nolint:errcheck // a temp file that outlives the pane is not worth an error path
	defer os.Remove(name)

	body, rerr := os.ReadFile(name) //nolint:gosec // our own temp file
	switch {
	case err != nil:
		msg.err = err
	case rerr != nil:
		msg.err = rerr
	default:
		msg.body = strings.TrimRight(string(body), "\n")
	}

	m.applyEdit(msg)

	return tea.ClearScreen
}

// paneFloor is the fewest rows the pane's program can run in; below it even a
// prompt line would not fit.
const paneFloor = 4

// paneHeight is how many rows the pane's program gets, below its divider.
// Half the frame leaves the diff readable above it.
func (m *Model) paneHeight() int {
	return max(paneFloor, m.height/2-1)
}

func editorArgv(path string) []string {
	return editorLineArgv(path, 0)
}

// editorLineArgv adds the +N vi and its descendants read as "open on line N",
// which is what puts a whole file at the hunk's line rather than its top. A
// zero asks for the top, which is what a file row carries.
func editorLineArgv(path string, at int) []string {
	fields := strings.Fields(os.Getenv("EDITOR"))
	if len(fields) == 0 {
		fields = []string{"vi"}
	}

	if at > 0 {
		fields = append(fields, "+"+strconv.Itoa(at))
	}

	return append(fields, path)
}

func (m *Model) applyEdit(msg editedMsg) {
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	// The buffer has landed, or it was empty and there is nothing to offer
	// back, so what was kept on disk for it goes.
	m.drop(m.draftKey(msg))

	// A review's body is emptied deliberately; a comment's body left empty is
	// an editor closed without saving, which is a cancel rather than a change.
	if msg.body == "" && msg.field == fieldBody && msg.index != reviewBody {
		m.say("empty body, nothing changed", false)

		return
	}

	if msg.fresh != nil {
		m.stageFresh(msg)

		return
	}

	// Both start closed, so writing one has to open it or the editor looks like
	// it discarded what was typed.
	switch msg.index {
	case reviewBody:
		m.review.Body = msg.body
		m.folded.notes[reviewBody] = true
		m.save("review body updated")

		return
	case reviewNote:
		m.review.Note = msg.body
		m.folded.notes[reviewNote] = true
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
		c.Origin = artifact.OriginMe
		m.save("note on " + c.ID + " updated; it stays local")

		return
	}

	if c.Body == msg.body {
		if promote(c) {
			m.save(c.ID + " is ready")

			return
		}

		m.say("unchanged", false)

		return
	}

	c.Body = msg.body
	c.Origin = artifact.OriginMe

	if promote(c) {
		m.save("edited " + c.ID + ", now ready")

		return
	}

	m.save("edited " + c.ID)
}

// promote makes a draft ready, and reports whether it did. Saving an edit is
// the ruling a draft is waiting for, which is the same reason a reply typed
// here is staged ready. A skip is left alone: it is a decision with a reason
// against it, and posting one somebody wrote out is the opposite of what x said.
func promote(c *artifact.Comment) bool {
	if c.Status != artifact.StatusDraft {
		return false
	}

	c.Status = artifact.StatusReady

	return true
}

// stageReply puts an answer to an open thread into the prepared review. It is
// staged ready rather than draft: a draft is a comment nobody has ruled on, and
// this one was typed by the person the screen belongs to.
func (m *Model) stageReply(msg editedMsg) {
	if msg.replyTo < 0 || msg.replyTo >= len(m.threads) {
		return
	}

	t := &m.threads[msg.replyTo]

	id := replyID(t.ReplyTo())
	for i := range m.review.Comments {
		if m.review.Comments[i].ID == id && m.review.Comments[i].Body == msg.body {
			m.say("unchanged", false)

			return
		}
	}

	m.review.Upsert(artifact.Comment{
		ID: id, Path: t.Path, Side: t.Side, Line: t.Line,
		InReplyTo: t.ReplyTo(), ThreadID: t.ID, Body: msg.body, Severity: question, Status: artifact.StatusReady,
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

// sendOne posts the comment under the cursor on its own. A ready comment asks
// nothing first: a single comment is the one thing here small enough to take
// back by deleting it on GitHub, where a whole review is not. A todo reply asks
// who gets the fix, as the submit chord does.
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

	switch c := &m.review.Comments[i]; {
	case c.Status == artifact.StatusReady:
		return m.sendWith(c.ID, HandNobody)
	case c.TodoReply():
		m.fixOne = c.ID
		m.askFixes("P  1 todo")
	default:
		m.say("only a ready comment or a todo reply goes out on its own; r marks it ready", false)
	}

	return nil
}

// sendWith posts comment id on its own and then hands its fix to hand.
func (m *Model) sendWith(id string, hand Hand) tea.Cmd {
	ctx, review := m.ctx, m.review
	handOff := m.handOff(ctx, hand, review.Only(id))

	m.say("posting "+id+"…", false)

	return func() tea.Msg {
		summary, err := m.send(ctx, review, id)
		if err != nil {
			return sentMsg{id: id, summary: summary, err: err}
		}

		summary, handErr := withHandOff(summary, handOff)

		return sentMsg{id: id, summary: summary, handErr: handErr}
	}
}

func (m *Model) applySent(msg sentMsg) {
	if msg.err != nil {
		m.failure = msg.err
		m.say(msg.err.Error(), true)

		return
	}

	m.rebuild()
	m.say(msg.summary, msg.handErr != nil)
}

// askSubmit opens the submit chord. Posting is the only thing the screen does
// that cannot be taken back, so the second key both confirms it and says what
// kind of review it is: Sa, Sr, and Sc each name one, and naming it is the only
// way to post.
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

	if m.review.Local {
		m.say("a local review's comments are notes; there is nothing to post to", true)

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

	m.pending = 'S'
	// The pull request is already named in the title bar, so the prompt spends
	// its width on what the keys do and stays readable in an 80-column frame.
	m.say("S  "+plural(c.ready, "comment")+"  "+hintLine(events()), false)
}

// submitAs answers the second key of the submit chord. A review with a todo
// reply asks one more thing before it posts: who gets the fixes.
func (m *Model) submitAs(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var event string

	switch {
	case msg.String() == "a":
		event = artifact.EventApprove
	case msg.String() == "r":
		event = artifact.EventRequestChanges
	case msg.String() == "c":
		event = artifact.EventComment
	default:
		m.say("canceled, nothing was posted", false)

		return m, nil
	}

	if len(m.review.TodoReplies()) > 0 {
		m.askFixesTo(event, msg.String())

		return m, nil
	}

	return m.submitWith(event, HandNobody)
}

func (m *Model) submitWith(event string, hand Hand) (tea.Model, tea.Cmd) {
	// The artifact records what was sent, so a review posted as an approval
	// does not read afterwards as one posted as a comment.
	if m.review.Event != event {
		m.review.Event = event
		if err := artifact.Save(m.path, m.review); err != nil {
			m.say(err.Error(), true)

			return m, nil
		}

		m.wrote, _ = stampOf(m.path)
	}

	m.posting = true

	m.say("posting as "+event+"…", false)

	ctx, review, handOff := m.ctx, m.review, m.handOff(m.ctx, hand, m.review)

	return m, func() tea.Msg {
		summary, err := m.submit(ctx, review)
		if err != nil {
			return submittedMsg{summary: summary, err: err}
		}

		summary, handErr := withHandOff(summary, handOff)

		return submittedMsg{summary: summary, handErr: handErr}
	}
}

// confirmKind is which confirmation owns the keyboard. Three of the screen's
// keys send something that cannot be taken back, and each is confirmed by its
// own key rather than by any keystroke at all.
type confirmKind int

const (
	askNothing confirmKind = iota
	askMerge
	askDeleteBranch
	askAdvisories
	askFixes
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

// answer reads the reply to whichever confirmation is waiting. Each kind owns
// its own key, so a mistyped reply to one never fires the other.
func (m *Model) answer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	kind := m.asking
	m.asking = askNothing

	switch kind {
	case askMerge:
		return m.answerMerge(msg)
	case askDeleteBranch:
		return m.answerDeleteBranch(msg)
	case askAdvisories:
		return m.answerAdvisories(msg)
	case askFixes:
		return m.answerFixes(msg)
	default:
		return m, nil
	}
}

// answerMerge reads the reply to the merge confirmation. Anything but the
// same key again cancels and is swallowed, so no keystroke meant for the
// review merges.
func (m *Model) answerMerge(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

// askDeleteBranchNow asks before it checks out the base branch and deletes
// this one locally. It is local housekeeping rather than anything GitHub
// sees, so unlike merge it is offered even while a review is still staged:
// nothing here strands work, it only moves the checkout off it.
func (m *Model) askDeleteBranchNow() {
	switch {
	case m.cleanup == nil:
		m.say("deleting the branch is not available here", true)
	case m.deleting:
		m.say("deleting…", false)
	case m.branchDeleted:
		m.say("already deleted", false)
	default:
		m.asking = askDeleteBranch
		m.say("D again to check out the base branch and delete "+m.review.HeadRef+" locally, any key cancels", false)
	}
}

// answerDeleteBranch reads the reply to the delete-branch confirmation.
// Anything but the same key again cancels and is swallowed, so no keystroke
// meant for the review deletes anything.
func (m *Model) answerDeleteBranch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !key.Matches(msg, m.keys.DeleteBranch) {
		m.say("canceled, nothing was deleted", false)

		return m, nil
	}

	m.deleting = true

	m.say("deleting "+m.review.HeadRef+"…", false)

	ctx, review := m.ctx, m.review

	return m, func() tea.Msg {
		summary, err := m.cleanup(ctx, review)

		return branchDeletedMsg{summary: summary, err: err}
	}
}

// browse opens the pull request in whatever the desktop calls a browser. It
// runs in front of the screen rather than as a command, because the open
// returns as soon as the browser has been told and there is nothing to wait
// for.
func (m *Model) browse() {
	if m.browser == nil {
		m.say("opening a browser is not available here", true)

		return
	}

	if err := m.browser(m.ctx, m.review); err != nil {
		m.say(err.Error(), true)

		return
	}

	m.say("opened "+m.where()+" on GitHub", false)
}

func (m *Model) where() string {
	return fmt.Sprintf("%s/%s#%d", m.review.Owner, m.review.Repo, m.review.Number)
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
	for i := range m.screen.rows {
		if r := m.screen.rows[i]; r.head && r.comment == index {
			m.cursor = i
			m.reveal()

			return true
		}
	}

	return false
}

// settled reports a review nothing may write back to: it has posted, is
// posting, or its pull request has merged, and each of those removed the file
// on disk or is about to.
func (m *Model) settled() bool { return m.posted || m.posting || m.merged }

func (m *Model) applySubmit(msg submittedMsg) {
	m.posting, m.failure = false, msg.err
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	m.posted = true
	m.j.Event("review", "post")

	hint := msg.summary + "; enter reviews the next one staged, o opens it on GitHub, "
	if m.cleanup != nil && !m.branchDeleted {
		hint += "D deletes " + m.review.HeadRef + " locally, "
	}

	m.say(hint+"q leaves", msg.handErr != nil)
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
	m.j.Event("review", "merge")
	m.say(msg.summary+", press q to leave", false)
}

// applyBranchDeleted reports the cleanup. A failure leaves the checkout
// wherever it stopped, which the message names, rather than claiming success.
func (m *Model) applyBranchDeleted(msg branchDeletedMsg) {
	m.deleting = false
	if msg.err != nil {
		m.say(msg.err.Error(), true)

		return
	}

	m.branchDeleted = true
	m.say(msg.summary, false)
}

func (m *Model) rebuild() { m.rebuildNow() }

func (m *Model) rebuildNow() {
	// An open range is two row numbers, and every row number here is about to
	// mean something else.
	m.selected = nil

	lay, was := m.layout()
	m.screen = m.buildRows(lay)
	m.finish(was)
}

// layout is the state a build reads, gathered while it is still the model's:
// the rows a screen is made of are cheap to ask for and the build itself is
// not, which is the split that lets the build run off the input goroutine.
func (m *Model) layout() (layout, where) {
	was := m.here()

	if m.notes == nil {
		m.notes = noted(m.threads)
	}

	m.hunkCols = hunkWidths(m.diff)

	m.rereview = m.rereviewed()

	lay := layout{
		width: m.diffWidth(), hide: m.skipper(), fold: m.folded, notes: m.notes, lit: m.lit,
		split: m.sideBySide(), made: m.made,
		drifted: artifact.Drifted(m.review.Comments, m.diff),
		grown: func(path string, hunk int, span [2]int) ([]row, []row) {
			return m.surround(path, hunk, span[0], span[1])
		},
		progress: m.fileProgress,
		moved:    m.moves,
		atHead:   m.atHead,
		searches: m.headSearch != nil,
		asks:     askCards(m.plan.Asks), inFlight: m.inFlight, failedAsks: m.failedAsks,
		verdicts:  m.verdictWords(),
		answers:   m.rereview,
		known:     m.advisories,
		answered:  m.advised,
		cards:     m.cards,
		versioned: m.versioned,
	}
	if !m.asDiffed {
		lay.plan = m.plan.Chapters
		lay.folds = foldedPaths(m.plan.Chapters)
	}

	if m.touring() {
		lay.tour, lay.inline, lay.stops = true, !m.tourWide(), m.plan.Stops
	}

	lay.trouble = m.trouble
	lay.writing = m.editing != nil && m.editing.msg.index == reviewBody

	return lay, was
}

// buildRows is the screen a layout makes, which is the part of a rebuild big
// enough to leave the input goroutine on a huge diff.
func (m *Model) buildRows(lay layout) screen {
	switch m.view {
	case viewTrouble:
		return buildTrouble(m.diff, m.trouble, m.probing, m.troubled, lay)
	case viewComments:
		return buildList(m.review, m.diff, lay)
	case viewCode:
		return buildCode(m.review, m.diff, m.threads, lay)
	case viewThreads:
		return buildThreads(m.diff, m.threads, lay)
	case viewFile:
		lines, loaded := m.blobs[m.whole.path]
		return buildWhole(m.review, m.diff, m.threads, lay, m.whole, lines, loaded)
	case viewDiff:
		return build(m.review, m.diff, m.threads, lay)
	}

	return build(m.review, m.diff, m.threads, lay)
}

// finish settles a fresh screen: the peek splices back in, the read count
// updates, the cursor lands where the rebuild meant to keep it, and a landing
// that was still armed opens.
func (m *Model) finish(was where) {
	m.splicePeek()
	m.done = m.readHunks()

	m.stay(was)
	m.cursor = clamp(m.cursor, len(m.screen.rows)-1)
	m.follow()
	m.landOpen()

	if m.landing != 0 {
		m.land()
	}
}

// land puts the cursor on the thread the screen was opened at, once, on the
// first rebuild with rows to find it in. The conversation's first comment id
// is the thread's first note's, which is the id a reply quotes. A thread the
// review does not carry is said to rather than landed nowhere in silence.
func (m *Model) land() {
	id := m.landing
	m.landing = 0

	for i := range m.screen.rows {
		r := &m.screen.rows[i]
		if r.kind != rowThread || !r.head {
			continue
		}

		t := &m.threads[r.thread]
		if len(t.Notes) > 0 && t.Notes[0].ID == id {
			m.cursor = i
			m.reveal()

			return
		}
	}

	m.say("the conversation is not in the threads this review carries", true)
}

// readHunks is every hunk already marked read, hashed once per rebuild rather
// than once per row: a row that asked would hash its whole hunk to draw one
// line of it.
func (m *Model) readHunks() map[hunkAt]bool {
	if m.read == nil {
		return nil
	}

	out := map[hunkAt]bool{}

	for _, ref := range seen.Hunks(m.diff) {
		if m.read.Has(seen.Hunk(m.diff, ref.Path, ref.Hunk)) {
			out[hunkAt{path: ref.Path, hunk: ref.Hunk}] = true
		}
	}

	return out
}

// behind reports whether a row is work already done, which recedes so the eye
// finds what is left rather than counting glyphs down the margin. It is the
// idea worth taking from nightfox's dim_inactive.
func (m *Model) behind(r row) bool {
	return r.hunk != 0 && m.done[hunkAt{path: r.path, hunk: r.hunk}]
}

func (m *Model) moveBy(n int) {
	step := 1
	if n < 0 {
		step, n = -1, -n
	}

	for range n {
		m.cursor = m.stopFrom(m.cursor, step)
	}
}

// stopFrom is the nearest row past from, in step, that the cursor can rest on,
// or from itself when there is none.
func (m *Model) stopFrom(from, step int) int {
	for i := from + step; i >= 0 && i < len(m.screen.rows); i += step {
		if !passed(m.screen.rows[i]) {
			return i
		}
	}

	return from
}

// lastStop is the final row the cursor can rest on, so G does not land on a foot.
func (m *Model) lastStop() int {
	for i := len(m.screen.rows) - 1; i >= 0; i-- {
		if !passed(m.screen.rows[i]) {
			return i
		}
	}

	return len(m.screen.rows) - 1
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

// reveal places the cursor for a jump, stopping at the last full frame of rows
// so the end of the review is not scrolled into blankness.
//
// Landing on a comment or a thread centers its block: a finding or a
// conversation is explained by the code above the line it hangs from, and
// anchoring the block near the top puts that code off the frame. Everything
// else anchors near the top, since scrolling by
// the least that reaches a heading leaves it on the last line, which is the one
// place the content under it cannot be read.
func (m *Model) reveal() {
	h := m.viewHeight()

	if top, tall, ok := m.blockSpan(); ok && tall < h {
		const sides = 2

		m.offset = clamp(top-(h-tall)/sides, m.maxOffset())

		return
	}

	// A comment block opens with a blank row, so landing on one keeps a row
	// more: the line a comment is about is the context it cannot be read
	// without.
	margin := jumpMargin
	if m.cursor > 0 && m.screen.rows[m.cursor-1].kind == rowBlank {
		margin++
	}

	m.offset = clamp(m.cursor-margin, m.maxOffset())
}

// blockKey is what groups the rows of one comment or one thread together, kept
// as a kind and an index rather than a bare int since a thread's index and a
// comment's both start at zero and would otherwise collide.
type blockKey struct {
	kind rowKind
	id   int
}

// groupOf is the block row i belongs to, and false for a row that is not part
// of one: code, a heading, anything a jump does not land on as its head.
func (m *Model) groupOf(i int) (blockKey, bool) {
	if i < 0 || i >= len(m.screen.rows) {
		return blockKey{}, false
	}

	switch r := m.screen.rows[i]; r.kind {
	case rowComment:
		if r.comment < 0 {
			return blockKey{}, false
		}

		return blockKey{rowComment, r.comment}, true
	case rowThread:
		return blockKey{rowThread, r.thread}, true
	case rowPeek:
		return blockKey{rowPeek, 0}, true
	default:
		return blockKey{}, false
	}
}

// blockSpan is where the comment or thread block under the cursor starts and
// how tall it is. It reports false on anything that is neither, which is every
// row a jump lands on except a comment or a thread head.
func (m *Model) blockSpan() (int, int, bool) {
	at, ok := m.groupOf(m.cursor)
	if !ok {
		return 0, 0, false
	}

	top := m.cursor
	for top > 0 {
		k, ok := m.groupOf(top - 1)
		if !ok || k != at {
			break
		}

		top--
	}

	return top, m.blockEnd() - top + 1, true
}

// follow keeps the cursor on screen with a few rows of context either side,
// scrolling by the smallest amount that gets there. Landing on a comment or a
// thread reveals the rest of its block where the frame has room, since its
// first line is a heading and what is under it is the part worth reading.
func (m *Model) follow() {
	// Half a frame is the most a margin can be before the two bounds below
	// fight each other and the view flips on every keystroke.
	const sides = 2

	h := m.viewHeight()
	pad := min(scrollOff, (h-1)/sides)

	m.offset = min(m.offset, m.cursor-pad)
	m.offset = max(m.offset, min(m.blockEnd()+pad, m.cursor+h-1)-h+1)
	m.offset = clamp(m.offset, m.maxOffset())
	m.fitCursor()
}

// blockEnd is the last row of the comment or thread the cursor is in, or the
// cursor.
func (m *Model) blockEnd() int {
	at, ok := m.groupOf(m.cursor)
	if !ok {
		return m.cursor
	}

	end := m.cursor
	for {
		k, ok := m.groupOf(end + 1)
		if !ok || k != at {
			break
		}

		end++
	}

	return end
}

// viewHeight is the frame minus the header, the footer lines, and the pane
// when one is running.
func (m *Model) viewHeight() int {
	child := 0
	if m.pane != nil || (m.agentOpen && m.agent != nil) {
		child = m.paneHeight() + 1
	}

	return max(1, m.height-headerRows-child-len(m.footerLines()))
}

// maxOffset is how far the frame may scroll. It looks past the last row by
// overscroll, so the end of a review is not jammed against the footer, and by
// the editor's own height while one is open, so a box on the last row is drawn
// whole rather than cut off the bottom.
func (m *Model) maxOffset() int {
	room := overscroll
	if m.editing != nil {
		room += len(m.editorLines())
	}

	return max(0, len(m.screen.rows)-m.viewHeight()+room)
}

// clamp holds v inside [0, hi], which is every bound a row index has.
func clamp(v, hi int) int {
	return max(0, min(v, max(0, hi)))
}
