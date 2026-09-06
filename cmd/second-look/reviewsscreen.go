package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
	// remote is what the forge says of each row, read when the cursor stops on
	// one. A staged review is local work and the one thing the file cannot say
	// is whether the work is still wanted.
	remote map[string]prstate.State
	// asked is every row already read, so a cursor moving back and forth costs
	// one request per row rather than one per pass.
	asked map[string]bool
}

// Start reads the directory again and forgets what the forge said, which is
// what ctrl+r means here. The read is local, so it answers before the frame
// rather than through a message.
func (s *reviewsScreen) Start() tea.Cmd {
	rows, err := staged()
	if err != nil {
		return nil
	}

	s.rows = rows
	s.remote, s.asked = nil, nil

	return nil
}

// stateMsg is one row's remote state.
type stateMsg struct {
	key   string
	state prstate.State
}

// restedOn reads the forge's view of the row the cursor has stopped on. It is
// one request per staged review at most, and only for the rows looked at.
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
	answered, ok := msg.(stateMsg)
	if !ok {
		return nil, false
	}

	if s.remote == nil {
		s.remote = map[string]prstate.State{}
	}

	s.remote[answered.key] = answered.state

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
	"here marks the row this directory stands on and not here a row it cannot",
	"reach; the rest are reachable, which is what C moves onto.",
	"The second line is what the pull request is called, and the row says what the",
	"forge thinks of it once the cursor has rested on it: merged, closed, or the",
	"last review you left.",
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

// sections puts each stack in its own group, then splits what is left by where
// the review is kept, because that is what says whether the code under review is
// on this disk. Within a group recency is the order and no further bucket earns
// its place.
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

	here := make([]tui.Row, 0, len(alone))

	var away []tui.Row

	for i := range alone {
		row := s.reviewRow(&alone[i], now)

		if alone[i].Stray {
			away = append(away, row)

			continue
		}

		here = append(here, row)
	}

	out = append(out, tui.Section{Name: "staged", Rows: here})
	if len(away) > 0 {
		out = append(out, tui.Section{Name: "left in a working copy", Rows: away})
	}

	return out
}

// stackName says what the chain lands on and that its order is the reading
// order, since the group being a stack is the only reason it is not in the list
// underneath.
func stackName(st *prepared.Stack) string {
	return "stacked onto " + st.Onto + ", bottom first"
}

func (s *reviewsScreen) reviewRow(r *prepared.Review, now time.Time) tui.Row {
	return tui.Row{
		// The key names the repository as well as the number, since the same
		// number in two repositories is two rows.
		Key:   r.Where(),
		Left:  r.Where(),
		Repo:  r.Repository,
		Mid:   prepared.State(r),
		Age:   humanize.Ago(r.Modified, now),
		Tail:  s.tail(r),
		Under: title(r),
		// A review with a draft in it is the one to come back to, which is
		// what the unread mark means on this screen.
		Unread: r.Blocked() || r.Broken != "",
	}
}

// tail is what the review carries, or why it could not be read, with where this
// directory stands relative to it and what the forge says of it.
func (s *reviewsScreen) tail(r *prepared.Review) string {
	if r.Broken != "" {
		return r.Broken
	}

	parts := []string{prepared.Holds(r)}

	if word := s.treeWord(r); word != "" {
		parts = append(parts, word)
	}

	if word := s.remote[r.Where()].Word(); word != "" {
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

// reachable reports a row whose code this directory holds, which is what C acts
// on and what reading around the change and running it need.
func (s *reviewsScreen) reachable(r *prepared.Review) bool {
	return s.here != "" && strings.EqualFold(s.here, r.Repository)
}

// treeWord marks the row this directory stands on and the rows it cannot reach,
// and says nothing for the rest. In a tree of one repository every row is
// reachable, so saying so on each of them would be noise rather than an
// indicator.
func (s *reviewsScreen) treeWord(r *prepared.Review) string {
	switch {
	case !s.reachable(r):
		return "not here"
	case s.head != "" && s.head == r.HeadSHA:
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

		return s.counts(), false, nil
	case tui.ActMark, tui.ActBrowse, tui.ActReply, tui.ActResolve,
		tui.ActComment, tui.ActApprove, tui.ActSort:
		return "", false, errNotHere
	}

	return "", false, nil
}

// checkout leaves the screen to move this directory's working copy onto the
// row, refusing where the directory is a checkout of something else rather than
// moving a tree the review has nothing to do with.
func (s *reviewsScreen) checkout(key string) (string, bool, error) {
	for i := range s.rows {
		if s.rows[i].Where() != key {
			continue
		}

		if !s.reachable(&s.rows[i]) {
			return "", false, fmt.Errorf("%s: %w", key, errNotACheckoutOfIt)
		}

		owner, name, _ := strings.Cut(s.rows[i].Repository, "/")
		s.move = &ref{owner: owner, repo: name, number: s.rows[i].Number}

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
	errNotACheckoutOfIt = errors.New("this directory is a checkout of something else; " +
		"the rows marked here are the ones C can move")
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
