package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/agents"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/prstate"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

// errUnknownRow reports a row the screen no longer has data for, which is a
// fault in the screen rather than anything a person can act on.
var (
	errUnknownRow = errors.New("that row is no longer in the list")
	errUnreadable = errors.New("the file cannot be read")
)

// reviewsScreen lists what is staged under .second-look and opens one.
//
// The artifact is deleted the moment a review posts, so every row is unfinished
// work: either the review being written, or one whose head has since moved and
// which will refuse to post until it is prepared again.
type reviewsScreen struct {
	ctx  context.Context //nolint:containedctx // it bounds the reread a refresh makes
	rows []prepared.Review
	open *ref
	// move is the row C was pressed on.
	move *ref
	// here is the repository this directory is a checkout of and head is where
	// it stands, which is what says whether a row's code is reachable from here.
	here string
	head string
	// armed is the row d was pressed on. Discarding is the one thing here that
	// deletes work, and every comment staged in the row goes with it.
	armed string
	// remote is what the forge says of each row, asked for all of them when
	// the list opens and of a row the cursor rests on after a refresh. A
	// staged review is local work and the one thing the file cannot say is
	// whether the work is still wanted.
	remote map[string]prstate.State
	// asked is every row already read, so a cursor moving back and forth costs
	// one request per row rather than one per pass.
	asked map[string]bool
	// probe is the configured command listing an agent's live sessions, and
	// live is its last answer keyed by the session id a review records. A row
	// whose agent is asking a question is the one still moving.
	probe []string
	live  map[string]agents.Live
}

// Start reads the directory again and forgets what the forge said, which is
// what ctrl+r means here. The read is local and answers before the frame; the
// forge's view follows in one message so a dead row sinks before the cursor
// goes looking for it.
func (s *reviewsScreen) Start() tea.Cmd {
	rows, err := staged()
	if err != nil {
		return nil
	}

	s.rows = rows
	s.remote, s.asked = nil, nil

	return tea.Batch(s.askAll(), s.probeAgents())
}

// agentsMsg is every session the configured listing answered, keyed by the id
// a review records.
type agentsMsg map[string]agents.Live

// probeAgents asks the configured listing once for the whole screen. A listing
// nobody configured costs nothing and says nothing, the same as a tool that
// cannot name its sessions.
func (s *reviewsScreen) probeAgents() tea.Cmd {
	if len(s.probe) == 0 {
		return nil
	}

	argv := s.probe

	return func() tea.Msg {
		live, err := agents.List(s.ctx, argv)
		if err != nil {
			return nil
		}

		return agentsMsg(live)
	}
}

// stateMsg is one row's remote state.
type stateMsg struct {
	key   string
	state prstate.State
}

// statesMsg is every row's remote state, landed together so the list re-sorts
// once rather than under the cursor a row at a time.
type statesMsg map[string]prstate.State

// askAll reads the forge's view of every row. Dead rows sort last, which only
// works if the answer is known for all of them, not just the one under the
// cursor.
func (s *reviewsScreen) askAll() tea.Cmd {
	if s.asked == nil {
		s.asked = map[string]bool{}
	}

	rows := make([]prepared.Review, 0, len(s.rows))

	for i := range s.rows {
		if s.rows[i].Broken == "" {
			s.asked[s.rows[i].Where()] = true
			rows = append(rows, s.rows[i])
		}
	}

	if len(rows) == 0 {
		return nil
	}

	ctx := s.ctx

	return func() tea.Msg {
		states := map[string]prstate.State{}

		var mu sync.Mutex

		var wg sync.WaitGroup

		for i := range rows {
			wg.Add(1)

			go func() {
				defer wg.Done()

				got, err := prstate.Fetch(ctx, ".", rows[i].Repository, rows[i].Number)
				if err != nil {
					return
				}

				mu.Lock()
				states[rows[i].Where()] = got
				mu.Unlock()
			}()
		}

		wg.Wait()

		return statesMsg(states)
	}
}

// restedOn reads the forge's view of a row the opening sweep missed, which is
// a row added to the directory after this screen started.
func (s *reviewsScreen) restedOn(key string) tea.Cmd {
	if s.asked[key] {
		return nil
	}

	r, ok := s.rowFor(key)
	if !ok {
		return nil
	}

	if s.asked == nil {
		s.asked = map[string]bool{}
	}

	s.asked[key] = true
	repo, number := r.Repository, r.Number

	return func() tea.Msg {
		got, err := prstate.Fetch(s.ctx, ".", repo, number)
		if err != nil {
			// A state nobody could read leaves the row saying what it knows
			// locally, which is what the screen said before it asked.
			return stateMsg{key: key}
		}

		return stateMsg{key: key, state: got}
	}
}

// Absorb records a state that has answered. The screen has no loader, so this
// is the one message it takes.
func (s *reviewsScreen) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	if s.remote == nil {
		s.remote = map[string]prstate.State{}
	}

	switch answered := msg.(type) {
	case stateMsg:
		s.remote[answered.key] = answered.state
	case statesMsg:
		for key, state := range answered {
			s.remote[key] = state
		}
	case agentsMsg:
		s.live = answered
	default:
		return nil, false
	}

	return nil, true
}

func (s *reviewsScreen) rowFor(key string) (*prepared.Review, bool) {
	for i := range s.rows {
		if s.rows[i].Where() == key {
			return &s.rows[i], true
		}
	}

	return nil, false
}

// reviewsHints is the footer, which advertises only the keys this screen offers.
var reviewsHints = [][2]string{
	{enterKey, "open"},
	{"C", "checkout"},
	{"d", "discard"},
	{refreshKey, "refresh"},
	{"?", helpArg},
}

var reviewsHelp = helpFor(helpMove(), [][2]string{
	{enterKey, "open the review screen for it"},
	{"/", "narrow to the rows carrying a word; esc puts them back"},
	{"f, F", "read one repository across all three queues, and every one again"},
	{"C", "move this checkout onto it, pulling where it is already on the branch"},
	{"d", "throw the review away with everything cached for it; d again confirms"},
	{refreshKey, "read the directory again"},
}, helpLeave(), prose(
	"blocked means a comment is still a draft, which stops the submit.",
	"Every review here is unfinished: the file is deleted when it posts.",
	"A review with no checkout of its repository is listed in its own group and",
	"opens the same way, from the API.",
	"here marks the one row this directory is standing on; a C pressed on a",
	"row it cannot reach says why in the footer.",
	"The second line is what the pull request is called, and the row says what",
	"the forge thinks of it: merged, closed, or the last review you left.",
	"A pull request based on another one staged here is grouped with it, bottom",
	"first, which is the order the diffs read in.",
))

func (s *reviewsScreen) counts() string {
	blocked := 0
	for i := range s.rows {
		if s.rows[i].Blocked() {
			blocked++
		}
	}

	if blocked == 0 {
		return strconv.Itoa(len(s.rows)) + " staged"
	}

	return fmt.Sprintf("%d staged · %d blocked", len(s.rows), blocked)
}

// note is what the tab strip says: how many staged reviews are holding a
// draft, which is the number that means there is a decision to make here, and
// how many agents are waiting on an answer, which is the reason to open it.
func (s *reviewsScreen) note() string {
	blocked, waiting := 0, 0

	for i := range s.rows {
		if s.rows[i].Blocked() {
			blocked++
		}

		if live, ok := s.live[s.rows[i].Agent.Session]; ok && live.State == agents.Blocked {
			waiting++
		}
	}

	var parts []string
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", blocked))
	}
	if waiting > 0 {
		parts = append(parts, humanize.Plural(waiting, "agent")+" waiting")
	}

	return strings.Join(parts, " · ")
}

// sections puts each stack in its own group, then splits what is left by where
// the review is kept, because that is what says whether the code under review is
// on this disk. A stack keeps its reading order; the rest sort by what needs a
// hand, with dead work last.
func (s *reviewsScreen) sections() []tui.Section {
	now := time.Now()
	stacks, alone := prepared.Split(s.rows)

	out := make([]tui.Section, 0, len(stacks)+2)

	for i := range stacks {
		rows := make([]tui.Row, 0, len(stacks[i].Rows))
		for j := range stacks[i].Rows {
			rows = append(rows, s.reviewRow(&stacks[i].Rows[j], now))
		}

		out = append(out, tui.Section{Name: stackName(&stacks[i]), Rows: rows})
	}

	var here, away []prepared.Review

	for i := range alone {
		if alone[i].Stray {
			away = append(away, alone[i])
		} else {
			here = append(here, alone[i])
		}
	}

	sort.SliceStable(here, func(i, j int) bool { return s.rank(&here[i]) < s.rank(&here[j]) })
	sort.SliceStable(away, func(i, j int) bool { return s.rank(&away[i]) < s.rank(&away[j]) })

	hereRows := make([]tui.Row, 0, len(here))
	for i := range here {
		hereRows = append(hereRows, s.reviewRow(&here[i], now))
	}

	out = append(out, tui.Section{Name: "staged", Rows: hereRows})

	if len(away) > 0 {
		awayRows := make([]tui.Row, 0, len(away))
		for i := range away {
			awayRows = append(awayRows, s.reviewRow(&away[i], now))
		}

		out = append(out, tui.Section{Name: "left in a working copy", Rows: awayRows})
	}

	return out
}

// rank is where a row sorts in its group: what needs a hand first, what is
// ready next, then what is empty or unreadable, and dead work last. The sort
// is stable, so rows inside a level keep the recency order they were listed
// in.
const (
	rankBlocked = iota
	rankReady
	rankWaiting
	deadLast
)

func (s *reviewsScreen) rank(r *prepared.Review) int {
	if st, ok := s.remote[r.Where()]; ok && (st.Merged() || st.Closed()) {
		return deadLast
	}

	switch prepared.State(r) {
	case prepared.StateBlocked:
		return rankBlocked
	case prepared.StateReady:
		return rankReady
	default:
		return rankWaiting
	}
}

// stackName says what the chain lands on and that its order is the reading
// order, since the group being a stack is the only reason it is not in the list
// underneath.
func stackName(st *prepared.Stack) string {
	return "stacked onto " + st.Onto + ", bottom first"
}

func (s *reviewsScreen) reviewRow(r *prepared.Review, now time.Time) tui.Row {
	state := prepared.State(r)
	word, tone := s.remoteWord(r)

	return tui.Row{
		// The key names the repository as well as the number, since the same
		// number in two repositories is two rows.
		Key:   r.Where(),
		Left:  r.Where(),
		Repo:  r.Repository,
		Mid:   state,
		Tone:  stateTone(state),
		Age:   humanize.Ago(r.Modified, now),
		Tail:  s.tail(r),
		Under: title(r),
		// A review with a draft in it is the one to come back to, which is
		// what the unread mark means on this screen.
		Unread:     r.Blocked() || r.Broken != "",
		Remote:     word,
		RemoteTone: tone,
		Agent:      s.agentWord(r),
		AgentTone:  s.agentTone(r),
	}
}

// agentWord is what the session recorded on a review is doing, and empty where
// no session is recorded or the listing does not name it. A recorded session
// absent from the listing ended, which is nothing the row needs to say.
func (s *reviewsScreen) agentWord(r *prepared.Review) string {
	if r.Agent.Session == "" {
		return ""
	}

	live, ok := s.live[r.Agent.Session]
	if !ok || live.State == "" {
		return ""
	}

	return "agent " + live.State
}

// agentTone is how loud the session's state is: blocked is the one asking to
// be answered, done is work that landed, and the rest say a run is in motion.
func (s *reviewsScreen) agentTone(r *prepared.Review) tui.Tone {
	live, ok := s.live[r.Agent.Session]
	if !ok {
		return tui.ToneOrdinary
	}

	switch live.State {
	case agents.Blocked:
		return tui.ToneWarn
	case agents.Done:
		return tui.ToneGood
	default:
		return tui.ToneMuted
	}
}

// stateTone is how loud the state word is: a review that can post is the good
// news, one holding a draft is the decision to make, and one that cannot be
// read is the problem.
func stateTone(state string) tui.Tone {
	switch state {
	case prepared.StateReady:
		return tui.ToneGood
	case prepared.StateBlocked:
		return tui.ToneWarn
	case prepared.StateUnreadable:
		return tui.ToneBad
	default:
		return tui.ToneMuted
	}
}

// remoteWord is what the forge says of the row and how loud it should say it:
// a dead pull request makes the staged work moot, and a merge draws in the
// forge's own color for it.
func (s *reviewsScreen) remoteWord(r *prepared.Review) (string, tui.Tone) {
	state, ok := s.remote[r.Where()]
	if !ok {
		return "", tui.ToneOrdinary
	}

	switch {
	case state.Merged():
		return state.Word(), tui.ToneMerged
	case state.Closed():
		return state.Word(), tui.ToneBad
	case state.Mine == "APPROVED":
		return state.Word(), tui.ToneGood
	default:
		return state.Word(), tui.ToneMuted
	}
}

// tail is what the review carries, or why it could not be read, with whether
// this directory is standing on it.
func (s *reviewsScreen) tail(r *prepared.Review) string {
	if r.Broken != "" {
		return r.Broken
	}

	parts := []string{prepared.Holds(r)}

	if word := s.treeWord(r); word != "" {
		parts = append(parts, word)
	}

	return strings.Join(parts, " · ")
}

// title is what the pull request is called, read out of what `second-look get`
// cached at the head this review was staged against. A row that says only
// owner/repo#118 is a row nobody recognizes.
func title(r *prepared.Review) string {
	var about threads.About
	if err := artifact.LoadAbout(prepared.Root(r), r.HeadSHA, &about); err != nil {
		return ""
	}

	return about.Title
}

// reachable reports a row whose code this directory holds, which is what the
// "here" marker and reading around the change need.
func (s *reviewsScreen) reachable(r *prepared.Review) bool {
	return s.here != "" && strings.EqualFold(s.here, r.Repository)
}

// treeWord marks the one row this directory is standing on and says nothing
// for the rest: a queue of several repositories repeating "not here" on every
// row is noise rather than an indicator, and a C pressed where it cannot act
// says why in the footer.
func (s *reviewsScreen) treeWord(r *prepared.Review) string {
	if s.reachable(r) && s.head != "" && s.head == r.HeadSHA {
		return "here"
	}

	return ""
}

func (s *reviewsScreen) act(a tui.Action, row *tui.Row) (string, bool, error) {
	if a != tui.ActDiscard {
		s.armed = ""
	}

	switch a {
	case tui.ActChoose:
		return s.choose(row.Key)
	case tui.ActDiscard:
		return s.discard(row.Key)
	case tui.ActCheckout:
		return s.checkout(row.Key)
	case tui.ActRefresh:
		rows, err := staged()
		if err != nil {
			return "", false, err
		}

		s.rows = rows
		s.remote, s.asked = nil, nil

		return s.counts(), false, nil
	case tui.ActMark, tui.ActBrowse, tui.ActReply, tui.ActResolve,
		tui.ActComment, tui.ActApprove, tui.ActSort:
		return "", false, errNotHere
	}

	return "", false, nil
}

// checkout leaves the screen to move a checkout of the row's repository onto
// it. Which clone is picked after the screen gives the terminal back, from
// every clone the laptop holds rather than only this directory.
func (s *reviewsScreen) checkout(key string) (string, bool, error) {
	for i := range s.rows {
		if s.rows[i].Where() != key {
			continue
		}

		if s.rows[i].Repository == "" {
			return "", false, fmt.Errorf("%s: %w", key, errNoRepoNamed)
		}

		owner, name, _ := strings.Cut(s.rows[i].Repository, "/")
		s.move = &ref{owner: owner, repo: name, number: s.rows[i].Number, head: s.rows[i].HeadRef}

		return "checking out " + key, true, nil
	}

	return "", false, fmt.Errorf("%w: %s", errUnknownRow, key)
}

// discard takes the key twice, and throws away the staged review along with the
// diff, threads, rating, and read marks kept for it. Nothing here posted, so
// what goes is the only copy.
func (s *reviewsScreen) discard(key string) (string, bool, error) {
	if s.armed != key {
		s.armed = key

		return "d again to discard " + key + " and everything cached for it", false, nil
	}

	s.armed = ""

	for i := range s.rows {
		if s.rows[i].Where() != key {
			continue
		}

		if err := prepared.Discard(&s.rows[i]); err != nil {
			return "", false, fmt.Errorf("discarding %s: %w", key, err)
		}

		s.rows = append(s.rows[:i], s.rows[i+1:]...)

		return "discarded " + key + "; " + s.counts(), false, nil
	}

	return "", false, fmt.Errorf("%w: %s", errUnknownRow, key)
}

// The keys another list screen offers and this one does not. Saying so beats a
// key that silently does nothing.
var (
	errNotHere = errors.New("that key belongs to the conversation queue; " +
		"enter opens a review, ? lists the keys")
	errNotInInbox = errors.New("that key belongs to the conversation queue; " +
		"enter reviews the pull request, o opens it on GitHub, ? lists the keys")
	errNoPullRequest  = errors.New("this row is a search that failed, not a pull request")
	errNoCheckoutHere = errors.New("no clone of it is on this laptop, so there is nothing to check out; " +
		"enter reviews it from the API instead")
	errEveryCloneLeased   = errors.New("every clone of it is leased by another second-look")
	errNoRepoNamed        = errors.New("names no repository to find a clone of")
	errNotOnAConversation = errors.New("that key belongs to the inbox, which lists pull requests; " +
		"r answers this conversation and R marks it dealt with")
)

// choose leaves the screen so the review can open. Two Bubble Tea programs
// cannot own the terminal at once, so which review was chosen is carried out
// rather than the screen opening it from inside.
func (s *reviewsScreen) choose(key string) (string, bool, error) {
	for i := range s.rows {
		if s.rows[i].Where() != key {
			continue
		}

		if s.rows[i].Broken != "" {
			return "", false, fmt.Errorf("%s: %w: %s", key, errUnreadable, s.rows[i].Broken)
		}

		owner, name, _ := strings.Cut(s.rows[i].Repository, "/")
		s.open = &ref{owner: owner, repo: name, number: s.rows[i].Number}

		return "opening " + key, true, nil
	}

	return "", false, fmt.Errorf("%w: %s", errUnknownRow, key)
}
