package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/notice"
	"github.com/kyleking/second-look/internal/resolve"
	"github.com/kyleking/second-look/internal/snapshot"
	"github.com/kyleking/second-look/internal/tui"
)

// threadsScreen is the conversation queue on screen: read one, mark it read,
// resolve it, or hand off to the review screen to answer it.
type threadsScreen struct {
	ctx    context.Context //nolint:containedctx // it bounds the gh calls the keys make
	queue  *conversations.Queue
	looked *conversations.Looked
	path   string
	repo   string
	// buckets is a snapshot, taken when the queue is read and again on a
	// refresh. Which bucket a conversation is in turns on whether it has been
	// read, so recomputing it after every mark would move the row out from
	// under the cursor the moment you opened it.
	buckets []conversations.Bucket
	// reply is the conversation a reply was asked for, read back once the screen
	// closes. Staging an answer means opening the review screen, which cannot
	// happen while this one owns the terminal.
	reply *conversations.Conversation
	// waiting is the fetch still out, and failed the one that was refused. A
	// queue that is empty because it has not answered yet says so rather than
	// reading as a queue with nothing in it.
	waiting bool
	failed  error
	// snaps is where the last queue was kept, nil in a test that has none.
	// records is what each pull request was read as, so the next read fetches
	// only what moved. have says rows are on screen, and stale says they are
	// from the snapshot and this run's read has not confirmed them. age is how
	// long ago the snapshot was saved.
	snaps   *snapshot.Store
	records map[string]conversations.Record
	have    bool
	stale   bool
	age     time.Duration
	// started is set by the first Start, so a later one is a refresh, which
	// reads everything again whatever the snapshot says.
	started bool
	// notices is the notification stream folded into this tab: everything
	// addressed to you that is not a conversation — mentions, review requests,
	// your own pull requests moving — with noteFailed holding a refused fetch
	// so it reads as refused rather than as empty.
	notices    []notice.Thread
	noteFailed error
	// noteStale says the notifications are the snapshot's, and noteDenied that
	// GitHub refused the stream to this token. denied is when it last did,
	// kept so the next open does not ask again within a day.
	noteStale  bool
	noteDenied bool
	denied     time.Time
	// gh runs the mutations marking a notification done or unsubscribing it,
	// held so a test can stand in for it.
	gh ghrun.Runner
	// local is what this laptop already holds for each pull request, read off
	// disk when the queue opens. A thread on a pull request with a review
	// staged here is work already started, which is what the inbox says too.
	local map[string]inbox.Known
}

// newThreadsScreen reads the read marks, which are local, and leaves the queue
// itself to the loader: fetching it is a network round trip and nothing should
// pay for it until the queue is looked at.
func newThreadsScreen(ctx context.Context, snaps *snapshot.Store) (*threadsScreen, error) {
	looked, err := loadLooked()
	if err != nil {
		return nil, err
	}

	path, err := conversations.LookedPath()
	if err != nil {
		return nil, fmt.Errorf("reading your read conversations: %w", err)
	}

	return &threadsScreen{
		ctx: ctx, queue: &conversations.Queue{}, looked: looked,
		path: path, repo: currentRepo(ctx), gh: ghrun.GH(), snaps: snaps,
	}, nil
}

// queueMsg is the conversation queue, answered or refused.
type queueMsg struct {
	read *conversations.Read
	err  error
}

// noticeMsg is the notification stream, answered or refused.
type noticeMsg struct {
	threads []notice.Thread
	err     error
}

// Start paints the last queue from the snapshot, then reads it again behind
// that, fetching only the pull requests that moved. The notification stream
// rides alongside, since a tab that knows half of what is owed to you still
// sends you to the browser for the rest, and is not asked for at all while
// GitHub's refusal of it is recent.
func (s *threadsScreen) Start() tea.Cmd {
	s.waiting = true
	s.local = localKnowledge()

	forced := s.started
	s.started = true

	var prior *conversations.Prior

	if !forced {
		prior = s.paint()
	}

	queue := func() tea.Msg {
		read, err := conversations.ReadQueue(s.ctx, ".", conversations.DefaultLimit, prior, time.Now())

		return queueMsg{read: read, err: err}
	}

	if s.noteDenied && !forced {
		return queue
	}

	return tea.Batch(queue, func() tea.Msg {
		threads, err := notice.Fetch(s.ctx, ".")

		return noticeMsg{threads: threads, err: err}
	})
}

// paint puts the snapshot on screen as rows nothing has confirmed yet, and says
// what it vouches for to the read that follows.
func (s *threadsScreen) paint() *conversations.Prior {
	if s.snaps == nil {
		return nil
	}

	snap, age, ok := s.snaps.Conversations()
	if !ok {
		return nil
	}

	s.queue = &snap.Queue
	s.buckets = conversations.Buckets(s.queue, s.looked)
	s.records = snap.Records
	s.have, s.stale, s.age = true, true, age
	s.denied = snap.Denied

	if snap.Refused(time.Now()) {
		s.noteDenied = true
	} else {
		s.notices, s.noteStale = snap.Notices, len(snap.Notices) > 0
	}

	return &conversations.Prior{Queue: snap.Queue, Records: snap.Records}
}

// keep writes what the screen holds as the snapshot the next open paints. A
// queue not yet read as anybody is not worth keeping.
func (s *threadsScreen) keep() {
	if s.snaps == nil || s.queue.Viewer == "" {
		return
	}

	s.snaps.SaveConversations(snapshot.Conversations{
		Queue: *s.queue, Records: s.records, Notices: s.notices, Denied: s.denied,
	})
}

func (s *threadsScreen) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	switch answered := msg.(type) {
	case noticeMsg:
		s.absorbNotices(answered)

		return nil, true
	case queueMsg:
		s.waiting = false

		if answered.err != nil {
			s.failed = answered.err

			return nil, true
		}

		s.failed = nil
		s.queue, s.records = answered.read.Queue, answered.read.Records
		s.buckets = conversations.Buckets(s.queue, s.looked)
		s.have, s.stale = true, false
		s.keep()

		return nil, true
	}

	return nil, false
}

// absorbNotices takes the stream's answer. A refusal is remembered rather than
// shown as an error, since it is the token and not the moment that failed.
func (s *threadsScreen) absorbNotices(answered noticeMsg) {
	s.noteStale = false

	switch {
	case errors.Is(answered.err, notice.ErrUnavailable):
		s.notices, s.noteFailed, s.noteDenied, s.denied = nil, nil, true, time.Now()
	case answered.err != nil:
		s.notices, s.noteFailed = nil, answered.err
	default:
		s.notices, s.noteFailed, s.noteDenied, s.denied = answered.threads, nil, false, time.Time{}
	}

	s.keep()
}

// save writes the read marks back and the counts the prompt reads. It runs on
// every mark rather than once on the way out: a crash mid-queue loses nothing,
// and the save merges over what is on disk, so a second open session's marks
// survive this one's writes.
func (s *threadsScreen) save() error {
	if err := conversations.SaveLooked(s.path, s.looked, s.queue.Conversations); err != nil {
		return fmt.Errorf("saving what you read: %w", err)
	}

	path, err := conversations.StatusPath()
	if err != nil {
		return fmt.Errorf("saving the queue counts: %w", err)
	}

	err = conversations.SaveStatus(path, conversations.Status{
		Updated: time.Now(),
		Unread:  s.unread(),
		Open:    len(s.queue.Conversations),
	})
	if err != nil {
		return fmt.Errorf("saving the queue counts: %w", err)
	}

	return nil
}

// sectionNotices is the heading of the notification stream.
const sectionNotices = "notifications"

// threadsHints is the footer, which advertises only the keys this screen offers.
var threadsHints = [][2]string{
	{enterKey, "read"},
	{"space", "done"},
	{"r", "reply"},
	{"R", "resolve"},
	{"o", "GitHub"},
	{"tab", "group"},
	{"?", helpArg},
}

var threadsHelp = helpFor(helpMove(), helpGroup(), [][2]string{
	{enterKey, "read the whole conversation, and mark it read; on a notification, open it"},
	{"space", "mark read without opening; on a notification, mark it done"},
	{"r", "leave and open the review screen to stage a reply"},
	{"R", "thumbs-up a conversation; on a notification, unsubscribe and dismiss it"},
	{"o", "open it on GitHub"},
	{refreshKey, "read the queue again"},
}, helpLeave(), prose(
	"● marks a conversation that moved since you last read it, and a ● beginning",
	"the status says a review of that pull request is already staged here.",
	"A reply is staged into that pull request's prepared review and posts with it,",
	"so r leaves the queue and opens the review screen for it. Any repository will",
	"do, cloned here or not: gh-repo-dashboard says which clones are on this laptop,",
	"and a repository with none is reviewed from the API.",
	"",
	"The notifications section is what GitHub's stream sends you — mentions, review",
	"requests, your own pull requests moving — so the browser stops being where you",
	"find out. Done marks the thread read there; unsubscribe never hears from it again.",
))

// counts is the header's right-hand corner: how much is in the queue, and how
// much of it is still unread. The unread figure is live rather than from the
// snapshot, so it falls as rows are read.
func (s *threadsScreen) counts() string {
	if s.failed != nil {
		return "the search failed"
	}

	if s.waiting {
		return searching
	}

	counts := fmt.Sprintf("%d conversations · %d unread", len(s.queue.Conversations), s.unread())
	if len(s.notices) > 0 {
		return fmt.Sprintf("%d notifications · %s", len(s.notices), counts)
	}

	return counts
}

// unread is how many conversations have moved since they were last read, which
// is the number the tab strip carries so a queue says what it holds without
// being switched to.
func (s *threadsScreen) unread() int {
	n := len(s.notices)

	for i := range s.queue.Conversations {
		if !s.looked.Since(&s.queue.Conversations[i]) {
			n++
		}
	}

	return n
}

// badge is the tab's count: the unread count once it is known, dim while it is
// the snapshot's, and nothing where there is no queue to count.
func (s *threadsScreen) badge() tui.Badge {
	if !s.have {
		return tui.Badge{}
	}

	if n := s.unread(); n > 0 {
		return tui.Badge{Label: strconv.Itoa(n), Stale: s.stale}
	}

	return tui.Badge{}
}

// state says what the header cannot leave to the rows: that the search is out
// or was refused.
func (s *threadsScreen) state() tui.State {
	switch {
	case s.failed != nil:
		return tui.State{Text: "the search failed"}
	case s.waiting:
		return tui.State{Text: cachedWord(s.age) + searching, Busy: true}
	}

	return tui.State{}
}

// sections turns the snapshot into rows. The unread mark is read live, so
// marking one clears its mark where it sits rather than moving it.
func (s *threadsScreen) sections() []tui.Section {
	if !s.have && (s.waiting || s.failed != nil) {
		return []tui.Section{{Name: "your conversations", Note: s.counts()}}
	}

	now := time.Now()
	shared := sharedNames(s.places())

	out := make([]tui.Section, 0, len(s.buckets)+1)

	switch {
	case len(s.notices) > 0:
		rows := make([]tui.Row, 0, len(s.notices))
		for i := range s.notices {
			rows = append(rows, noticeRow(&s.notices[i], shared, s.noteStale, now))
		}
		out = append(out, tui.Section{Name: sectionNotices, Rows: rows})
	case s.noteDenied:
		out = append(out, tui.Section{Name: sectionNotices, Note: "not available to this token", Recedes: true})
	case s.noteFailed != nil:
		out = append(out, tui.Section{Name: sectionNotices, Note: s.noteFailed.Error()})
	}

	for i := range s.buckets {
		b := &s.buckets[i]
		recedes := b.Name == conversations.BucketAwaiting
		rows := make([]tui.Row, 0, len(b.Items))

		for j := range b.Items {
			c := &b.Items[j]
			rows = append(rows, tui.Row{
				Key:    c.Key(),
				Repo:   c.Repository,
				Kind:   string(c.Kind),
				Unread: !s.looked.Since(c),
				Dim:    recedes,
				Stale:  s.stale,
				Cells:  s.conversationCells(c, shared, now),
				About:  s.conversationAbout(c, now),
				Detail: detail(c),
			})
		}

		out = append(out, tui.Section{Name: b.Name, Rows: rows, Recedes: recedes})
	}

	return out
}

// noticeRow draws a notification like the queue's own rows: where and what it
// is at the edges, why it came in the status, and the unread mark it earns by
// arriving through a fetch that only asks for unread.
func noticeRow(n *notice.Thread, shared map[string]bool, stale bool, now time.Time) tui.Row {
	return tui.Row{
		Key: n.Key(), Repo: n.Repo, Kind: noticeKind(n.Kind), Unread: true, Stale: stale,
		Cells: noticeCells(n, shared, now), About: noticeAbout(n, now),
	}
}

// noticeKind is the subject type in the words the kind: filter says.
func noticeKind(kind string) string {
	if kind == "PullRequest" {
		return "pr"
	}

	return strings.ToLower(kind)
}

// noticeReason is why the notification arrived, in the words a row has room for.
func noticeReason(reason string) string {
	switch reason {
	case "review_requested":
		return "review asked"
	case "state_change":
		return "state changed"
	case "ci_activity":
		return "ci"
	case "author":
		return "your pr"
	case "team_mention":
		return "team mention"
	default:
		return reason
	}
}

// errNotANotification is what r and the other conversation gestures answer on
// a notification, which carries no thread to answer and no pull request here
// to act on.
var errNotANotification = errors.New("a notification is only opened, done, or unsubscribed")

// notice finds the notification a row stands for, or nil where the key is not one.
func (s *threadsScreen) notice(key string) *notice.Thread {
	for i := range s.notices {
		if s.notices[i].Key() == key {
			return &s.notices[i]
		}
	}

	return nil
}

// detail is the whole conversation, shown when a row is expanded. Every comment
// is there in order: the point of the queue is answering what was said, and the
// first line of the last one is rarely enough to do that.
func detail(c *conversations.Conversation) []string {
	var out []string

	for i := range c.Notes {
		n := &c.Notes[i]

		out = append(out, n.Author+":")

		for line := range strings.SplitSeq(strings.ReplaceAll(n.Body, "\r\n", "\n"), "\n") {
			out = append(out, "  "+line)
		}
	}

	return out
}

// find is the conversation a row stands for.
func (s *threadsScreen) find(key string) *conversations.Conversation {
	for i := range s.queue.Conversations {
		if s.queue.Conversations[i].Key() == key {
			return &s.queue.Conversations[i]
		}
	}

	return nil
}

func (s *threadsScreen) act(a tui.Action, row *tui.Row) (string, bool, error) {
	if n := s.notice(row.Key); n != nil {
		return s.actNotice(a, n)
	}

	c := s.find(row.Key)
	if c == nil {
		return "", false, fmt.Errorf("%w: %s", errUnknownRow, row.Key)
	}

	switch a {
	case tui.ActMark, tui.ActChoose:
		s.looked.Mark(c, time.Now())

		if err := s.save(); err != nil {
			return "", false, err
		}

		return "read " + c.Where() + " " + c.Anchor(), false, nil
	case tui.ActBrowse:
		return s.browse(c)
	case tui.ActResolve:
		return s.resolve(c)
	case tui.ActReply:
		return s.stageReply(c)
	case tui.ActRefresh:
		return "", false, nil
	case tui.ActCheckout, tui.ActComment, tui.ActApprove, tui.ActDiscard, tui.ActSort,
		tui.ActMerge, tui.ActFold:
		return "", false, errNotOnAConversation
	}

	return "", false, nil
}

func (s *threadsScreen) browse(c *conversations.Conversation) (string, bool, error) {
	if err := s.gh.Run(s.ctx, ".", "browse", "--repo", c.Repository, strconv.Itoa(c.Number)); err != nil {
		return "", false, fmt.Errorf("opening %s: %w", c.Where(), err)
	}

	return "opened " + c.Where(), false, nil
}

// actNotice runs the gestures a notification has: enter or o opens the subject
// on GitHub, space marks the thread done, R unsubscribes it. Opening leaves the
// row standing — reading it is not the same as being done with it.
func (s *threadsScreen) actNotice(a tui.Action, n *notice.Thread) (string, bool, error) {
	switch a {
	case tui.ActChoose, tui.ActBrowse:
		args := []string{"browse", "--repo", n.Repo}
		if n.Number > 0 {
			args = append(args, strconv.Itoa(n.Number))
		}

		if err := s.gh.Run(s.ctx, ".", args...); err != nil {
			return "", false, fmt.Errorf("opening %s: %w", n.Where(), err)
		}

		return "opened " + n.Where(), false, nil
	case tui.ActMark:
		if err := notice.Done(s.ctx, s.gh, *n); err != nil {
			return "", false, fmt.Errorf("marking %s done: %w", n.Where(), err)
		}

		s.dropNotice(n.Key())

		return "done " + n.Where(), false, nil
	case tui.ActResolve:
		if err := notice.Ignore(s.ctx, s.gh, *n); err != nil {
			return "", false, fmt.Errorf("unsubscribing %s: %w", n.Where(), err)
		}

		s.dropNotice(n.Key())

		return "unsubscribed " + n.Where(), false, nil
	default:
		return "", false, errNotANotification
	}
}

// dropNotice takes a notification out of the list once it is done.
func (s *threadsScreen) dropNotice(key string) {
	kept := make([]notice.Thread, 0, len(s.notices))

	for i := range s.notices {
		if s.notices[i].Key() != key {
			kept = append(kept, s.notices[i])
		}
	}

	s.notices = kept
}

// resolve marks the conversation dealt with on GitHub and drops it from the
// queue, since the queue is what is still open and this no longer is.
func (s *threadsScreen) resolve(c *conversations.Conversation) (string, bool, error) {
	status, err := resolve.Run(s.ctx, s.gh, ".", c)
	if err != nil {
		return "", false, fmt.Errorf("marking %s dealt with: %w", c.Where(), err)
	}

	s.drop(c.Key())

	return status, false, nil
}

// drop takes a conversation out of the queue and out of the snapshot, which is
// what a resolve leaves behind: the queue is what is still open, and this is not.
func (s *threadsScreen) drop(key string) {
	kept := make([]conversations.Conversation, 0, len(s.queue.Conversations))

	for i := range s.queue.Conversations {
		if s.queue.Conversations[i].Key() != key {
			kept = append(kept, s.queue.Conversations[i])
		}
	}

	s.queue.Conversations = kept

	for i := range s.buckets {
		rows := make([]conversations.Conversation, 0, len(s.buckets[i].Items))

		for j := range s.buckets[i].Items {
			if s.buckets[i].Items[j].Key() != key {
				rows = append(rows, s.buckets[i].Items[j])
			}
		}

		s.buckets[i].Items = rows
	}
}

// stageReply hands off to the review screen, which is where a reply is written
// and staged into the prepared review. Doing it here would mean a second editor
// flow and a second copy of the anchor rules, and the review screen already
// answers a thread with e.
//
// Which directory the review opens in is decided outside the screen, because
// finding a checkout of another repository can ask a question and this screen
// owns the terminal.
func (s *threadsScreen) stageReply(c *conversations.Conversation) (string, bool, error) {
	if c.Kind != conversations.KindThread {
		return "", false, fmt.Errorf("%s: %w", c.Anchor(), errNotAThread)
	}

	s.reply = c
	s.looked.Mark(c, time.Now())

	if err := s.save(); err != nil {
		return "", false, err
	}

	return "opening " + c.Where(), true, nil
}
