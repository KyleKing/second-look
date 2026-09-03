package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/forge/github"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/cost"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/structure"
	"github.com/kyleking/second-look/internal/tui"
)

// inboxScreen is the review queue on screen: the three buckets, and enter to
// review whichever pull request the cursor is on.
//
// Opening one costs an API read rather than a clone and a branch switch, which
// is the whole reason a queue is faster than a browser tab. A repository this
// laptop has a clone of is still reviewed there, because that is where the diff
// cache, the read marks, and an agent already look.
type inboxScreen struct {
	ctx     context.Context //nolint:containedctx // it bounds the searches a refresh makes
	buckets []inbox.Bucket
	// waiting counts the searches still out, so the header can say the queue is
	// short because it is not finished rather than because it is quiet.
	waiting int
	// plan is the searches the queue makes, closed over the config read before
	// the screen opened: a config error has nowhere to be printed while the
	// alternate screen is up.
	plan func() []inbox.Bucket
	// next is what the screen was left to do. Reviewing, checking out, and
	// writing a comment all need the terminal this screen owns, so each is
	// carried out and performed once it has closed.
	next *handoff
	// configured says the sections came from the config, which is what decides
	// whether the first one can be called what is waiting on you.
	configured bool
	// local is what this laptop already holds for each pull request: a prepared
	// review, and what its cached diff was rated. It is read once when the
	// screen opens, since ordering a queue by it must cost no API calls.
	local map[string]inbox.Known
	// armed is the row A was pressed on. Approving is the one thing here that
	// cannot be taken back by deleting something, so it takes the key twice.
	armed string
	// ratings is what earlier runs made of these pull requests, read off disk
	// when the screen opens and written back when the last row is rated, and
	// asked is every row one of them already fetched the diff of.
	ratings artifact.Ratings
	asked   map[string]bool
	// rated carries a row's cost back from the pool that fetched its diff, and
	// slots is how many of those may run at once. An API read per row is worth
	// the order it buys and not worth eighty at once.
	rated chan costMsg
	slots chan struct{}
	// waiting counts the ratings still out, and listening marks the one command
	// reading them, since two would each take a message and re-issue.
	pending   int
	listening bool
	// budget is what is left of GitHub's hourly allowance, nil until the read
	// answers, and queued is the rows waiting on that answer. Rating is the one
	// thing here that makes a burst of reads nobody asked for, so it asks what
	// it can afford before it starts.
	budget *github.Allowance
	queued []inbox.PullRequest
	// spent is what this run has already committed of the allowance.
	spent int
	// short marks a run that left rows unrated because the hourly allowance
	// would not cover them, which is a different thing from a diff that could
	// not be read and wants saying differently.
	short bool
	// unread counts the rows whose diff could not be fetched, which is what a
	// rate limit or a dropped connection looks like. Saying so beats a queue
	// that quietly orders itself by age and looks like it never tried.
	unread int
}

// howManyAtOnce bounds the diffs the rating pool fetches. It is small because
// each row is a network read the reader did not ask for: the queue is already
// drawn and this only reorders it.
const howManyAtOnce = 4

// costMsg is one row's rating.
type costMsg struct {
	key  string
	when time.Time
	cost int
	// read says the diff was fetched, whatever the grammar then made of it, and
	// rated says a grammar answered. A row that was read and not rated is still
	// recorded, so it is not fetched again until it is pushed to; a row that
	// could not be read is not, since the next open may reach it.
	read  bool
	rated bool
}

// handoff is an action the screen carried out with the row it was pressed on.
type handoff struct {
	act tui.Action
	at  ref
}

var inboxHints = [][2]string{
	{enterKey, "review"},
	{"C", "check out"},
	{"m", "comment"},
	{"A", "approve"},
	{"o", "GitHub"},
	{"?", helpArg},
}

var inboxHelp = helpFor(helpMove(), helpGroup(), [][2]string{
	{enterKey, "open the review screen for it"},
	{"C", "move a checkout onto it, asking before it stashes"},
	{"m", "comment on the pull request itself, in $EDITOR"},
	{"A", "approve it, A again to confirm"},
	{"o", "open it on GitHub"},
	{refreshKey, "run the searches again"},
}, helpLeave(), prose(
	"The buckets are the sections your config names, or the three built-in ones:",
	"waiting on you, then what you answered and is still open, then what merged.",
	"Opening one needs no checkout, and C is what gets one when this laptop has a",
	"clone. Merging is not here: it is M in the review screen, after reading it.",
	"They run at once and each is drawn as it lands, and a search that failed says",
	"so and leaves the others alone.",
))

// perform runs what the screen closed for. A failure is reported and the queue
// comes back, because a checkout that could not move or an editor that was
// closed empty is not a reason to lose the queue.
func perform(ctx context.Context, h *handoff, stdin io.Reader, stdout io.Writer) error {
	var err error

	switch h.act {
	case tui.ActCheckout:
		err = checkoutRef(ctx, h.at, stdin, stdout)
	case tui.ActComment:
		err = commentOn(ctx, h.at, stdout)
	case tui.ActChoose, tui.ActMark, tui.ActBrowse, tui.ActReply, tui.ActResolve,
		tui.ActRefresh, tui.ActApprove, tui.ActDiscard:
		return nil
	}

	if err == nil {
		return nil
	}

	return write(stdout, err.Error()+"\n")
}

// checkoutRef moves a working copy onto a pull request from the queue, which is
// the same verb C runs inside the review screen and asks the same question
// about uncommitted work.
func checkoutRef(ctx context.Context, at ref, stdin io.Reader, stdout io.Writer) error {
	t, err := get.Resolve(ctx, ".", at.owner, at.repo, at.number)
	if err != nil {
		return fmt.Errorf("checking out %s: %w", at, err)
	}

	if t.Detached() {
		return fmt.Errorf("%s: %w", at.owner+"/"+at.repo, errNoCheckoutHere)
	}

	if err := get.Prepare(ctx, stdout, t, confirm(stdin, stdout)); err != nil {
		return fmt.Errorf("checking out %s: %w", at, err)
	}

	return nil
}

// commentOn says something on the pull request itself rather than on a line of
// it, which is the one thing a queue row wants to say that a review comment
// cannot. It posts on its own: there is no diff here to anchor anything to.
func commentOn(ctx context.Context, at ref, stdout io.Writer) error {
	body, err := edit(ctx, "")
	if err != nil {
		return fmt.Errorf("commenting on %s: %w", at, err)
	}

	err = ghrun.GH().Run(ctx, ".", "pr", "comment", strconv.Itoa(at.number),
		"--repo", at.owner+"/"+at.repo, "--body", body)
	if err != nil {
		return fmt.Errorf("commenting on %s: %w", at, err)
	}

	return write(stdout, "commented on "+at.String()+"\n")
}

// bucketMsg is one search that has answered, and where it belongs.
type bucketMsg struct {
	at     int
	bucket inbox.Bucket
}

// Start puts the headings up and sends every search off at once. Four sections
// run one after another cost the sum of four searches where the slowest alone
// is under two seconds, and an empty terminal until then reads as a hang.
func (s *inboxScreen) Start() tea.Cmd {
	s.local = localKnowledge()
	if s.local == nil {
		s.local = map[string]inbox.Known{}
	}

	s.ratings = artifact.LoadRatings()
	s.asked = map[string]bool{}
	s.buckets = s.plan()
	s.waiting = len(s.buckets)
	s.armed = ""
	s.pending, s.listening, s.unread = 0, false, 0
	s.budget, s.queued, s.spent, s.short = nil, nil, 0, false
	s.rated = make(chan costMsg, ratingBuffer)
	s.slots = make(chan struct{}, howManyAtOnce)

	cmds := make([]tea.Cmd, 0, len(s.buckets)+1)

	// The allowance is read alongside the searches rather than before them: it
	// is free and it answers faster than any of them, so nothing waits for it.
	cmds = append(cmds, func() tea.Msg {
		budget, err := github.Budgets(s.ctx, ".")

		return budgetMsg{left: budget.Core, err: err}
	})

	for i := range s.buckets {
		at, want := i, s.buckets[i]

		cmds = append(cmds, func() tea.Msg {
			return bucketMsg{at: at, bucket: inbox.Run(s.ctx, ".", want)}
		})
	}

	return tea.Batch(cmds...)
}

// ratingBuffer is how many answers the pool may leave waiting for the loop.
// It only has to outrun one keystroke, since the loop drains one per message.
const ratingBuffer = 64

// Absorb takes a search or a rating that has answered. It runs on the program's
// own loop, so this is the one place the buckets and what is known are written.
func (s *inboxScreen) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	switch answered := msg.(type) {
	case bucketMsg:
		return s.absorbBucket(answered), true
	case budgetMsg:
		return s.absorbBudget(answered), true
	case costMsg:
		return s.absorbCost(answered), true
	}

	return nil, false
}

func (s *inboxScreen) absorbBucket(answered bucketMsg) tea.Cmd {
	if answered.at >= len(s.buckets) {
		return nil
	}

	for key := range inbox.Recall(answered.bucket.Items, s.ratings, s.local) {
		s.asked[key] = true
	}

	inbox.Rank(answered.bucket.Items, s.known)

	s.buckets[answered.at] = answered.bucket
	s.waiting--

	return s.rate(answered.bucket.Items)
}

// absorbCost records one rating and puts the queue back in order around it. The
// list screen keeps the cursor on the row it was on, so a row moving under a
// reader who has started reading does not move the reader.
func (s *inboxScreen) absorbCost(answered costMsg) tea.Cmd {
	// An empty key is the canceled context, which is the screen closing rather
	// than a row that could not be rated.
	if answered.key == "" {
		s.listening = false

		return nil
	}

	s.pending--

	if answered.read {
		s.ratings[answered.key] = artifact.Rating{
			Updated: answered.when, Cost: answered.cost, Rated: answered.rated,
		}
	} else {
		s.unread++
	}

	if answered.rated {
		known := s.local[answered.key]
		known.Cost, known.Rated = answered.cost, true
		s.local[answered.key] = known

		for i := range s.buckets {
			inbox.Rank(s.buckets[i].Items, s.known)
		}
	}

	if s.pending > 0 {
		return s.listen()
	}

	s.listening = false

	// The file is written once the queue is rated rather than per row: it is
	// one file for every repository, so a write per answer would be eighty
	// rewrites of the same thing.
	if err := artifact.SaveRatings(s.ratings); err != nil {
		return nil
	}

	return nil
}

// rate sends every row nobody has rated to the pool, and returns the command
// that reads the answers back.
//
// A bucket short enough to read at a glance is left in the order its search
// answered, because a read per row buys nothing there. Rating also needs a
// grammar, so where none is installed nothing is fetched: a number built from
// no parsed hunk is a hunk count wearing a rating's clothes.
func (s *inboxScreen) rate(items []inbox.PullRequest) tea.Cmd {
	if len(items) < inbox.WorthRating || !structure.Available() {
		return nil
	}

	s.queued = append(s.queued, items...)

	return s.spend()
}

// budgetMsg is what is left of the hourly allowance a rating spends.
type budgetMsg struct {
	left github.Allowance
	err  error
}

// absorbBudget releases whatever was waiting on the answer. A read that failed
// is not a reason to leave a queue unordered, so the burst goes ahead: the
// worst it can do is what it did before anything asked.
func (s *inboxScreen) absorbBudget(answered budgetMsg) tea.Cmd {
	left := answered.left
	if answered.err != nil {
		left = github.Allowance{Limit: -1, Remaining: -1}
	}

	s.budget = &left

	return s.spend()
}

// spend starts the rows the allowance can pay for. Nothing runs until the
// allowance has answered, since a burst is the one thing here worth holding a
// second for.
//
// The pool has to cover the burst twice over, so the queue never spends more
// than half of what is left. Ordering a queue is what leads to opening the
// reviews in it, and those are reads too: a queue ordered perfectly by a pool
// with nothing left in it has spent the budget on the index and none on the
// book.
func (s *inboxScreen) spend() tea.Cmd {
	if s.budget == nil || len(s.queued) == 0 {
		return nil
	}

	want := s.queued
	s.queued = nil

	started := 0

	for i := range want {
		p := want[i]

		key := artifact.RatingKey(p.Repository, p.Number)
		if s.local[key].Rated || s.asked[key] {
			continue
		}

		if !s.affords(started + 1) {
			s.short = true

			break
		}

		started++

		go s.rateOne(key, p)
	}

	s.pending += started
	s.spent += started

	if started == 0 || s.listening {
		return nil
	}

	s.listening = true

	return s.listen()
}

// keepBack is how much of the allowance the queue leaves for what ordering it
// leads to. Spending at most half means the reviews the order names are still
// affordable: a queue sorted perfectly by an exhausted pool spent the budget on
// the index and none of it on the book.
const keepBack = 2

// affords reports whether the allowance covers n more reads with as much again
// left over. An allowance nobody could read covers everything, which is what
// the queue did before it asked.
func (s *inboxScreen) affords(n int) bool {
	if s.budget.Remaining < 0 {
		return true
	}

	return s.budget.Covers(keepBack * (s.spent + n))
}

func (s *inboxScreen) rateOne(key string, p inbox.PullRequest) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	answer := costMsg{key: key, when: p.Updated}

	if score, err := cost.Of(s.ctx, ".", p.Repository, p.Number); err == nil {
		answer.read = true
		answer.cost, answer.rated = score.Total, score.Rated()
	}

	select {
	case s.rated <- answer:
	case <-s.ctx.Done():
	}
}

// listen waits for one rating. A canceled context answers with a rating that
// records nothing, so the loop stops rather than waiting on a pool that is
// going away with the screen.
func (s *inboxScreen) listen() tea.Cmd {
	rated, done := s.rated, s.ctx.Done()

	return func() tea.Msg {
		select {
		case answered := <-rated:
			return answered
		case <-done:
			return costMsg{}
		}
	}
}

// known is what this laptop holds for one row of the queue.
func (s *inboxScreen) known(p *inbox.PullRequest) inbox.Known {
	return s.local[fmt.Sprintf("%s#%d", p.Repository, p.Number)]
}

// localKnowledge reads every review staged on this laptop and whatever each one
// rated its diff. A queue is ordered from it, so nothing here reaches GitHub
// and a failure leaves the queue in the order the searches answered.
func localKnowledge() map[string]inbox.Known {
	rows, err := staged()
	if err != nil {
		return nil
	}

	out := make(map[string]inbox.Known, len(rows))

	for i := range rows {
		r := &rows[i]

		total, rated := artifact.LoadScore(filepath.Dir(filepath.Dir(r.Path)), r.HeadSHA)
		out[artifact.RatingKey(r.Repository, r.Number)] = inbox.Known{
			Reviewed: true, Cost: total, Rated: rated,
		}
	}

	return out
}

// counts leads with the number worth acting on, which is what is waiting on you
// for the built-in buckets and the whole queue for sections somebody wrote: a
// configured first section is whatever its query asked for and calling it work
// waiting on you would be a guess.
//
// A failed search is named rather than left to make a short queue look quiet.
func (s *inboxScreen) counts() string {
	if s.waiting > 0 {
		return humanize.Plural(s.waiting, "search", "searches") + " still out"
	}

	// Rows move as ratings land, so the header says what is moving them.
	if s.pending > 0 {
		return humanize.Plural(s.pending, "row") + " still being rated"
	}

	if s.short {
		return s.budgetWord()
	}

	if s.unread > 0 {
		return humanize.Plural(s.unread, "row") + " could not be rated"
	}

	rows, failed := 0, 0

	for i := range s.buckets {
		if s.buckets[i].Err != "" {
			failed++

			continue
		}

		if !s.configured && i > 0 {
			continue
		}

		rows += len(s.buckets[i].Items)
	}

	out := fmt.Sprintf("%d waiting on you", rows)
	if s.configured {
		out = fmt.Sprintf("%d in %s", rows, humanize.Plural(len(s.buckets), "section"))
	}

	if failed == 0 {
		return out
	}

	return out + " · " + humanize.Plural(failed, "search", "searches") + " failed"
}

// budgetWord says why the order stopped where it did, and when it can be
// finished, since the reader can do nothing about it until then.
func (s *inboxScreen) budgetWord() string {
	out := "rated what the GitHub allowance covered"
	if s.budget == nil {
		return out
	}

	if wait := s.budget.In(time.Now()); wait > 0 {
		out += fmt.Sprintf("; %d reads left, more in %dm", s.budget.Remaining, int(wait.Minutes())+1)
	}

	return out
}

func (s *inboxScreen) sections() []tui.Section {
	now := time.Now()

	out := make([]tui.Section, 0, len(s.buckets))

	for i := range s.buckets {
		b := &s.buckets[i]

		if b.Pending() {
			out = append(out, tui.Section{Name: b.Name, Note: "searching…"})

			continue
		}

		if b.Err != "" {
			out = append(out, tui.Section{Name: b.Name, Rows: []tui.Row{{
				Left: "could not be read", Tail: humanize.FirstLine(b.Err),
			}}})

			continue
		}

		rows := make([]tui.Row, 0, len(b.Items))

		for j := range b.Items {
			p := &b.Items[j]
			key := fmt.Sprintf("%s#%d", p.Repository, p.Number)
			rows = append(rows, tui.Row{
				Key:  key,
				Left: key,
				Mid:  humanize.Clip(p.Author, authorCap),
				Age:  humanize.Ago(p.Updated, now),
				Cost: rated(s.local[key]),
				Tail: waiting(p),
			})
		}

		out = append(out, tui.Section{Name: b.Name, Rows: rows})
	}

	return out
}

const authorCap = 14

// rated is what an earlier read of this pull request made of it, which is the
// one number here that is about the change rather than about the queue. The
// column it is drawn in is labeled once by the header rather than on every
// row, since a queue is scanned down rather than read across.
func rated(k inbox.Known) string {
	if !k.Rated {
		return ""
	}

	return strconv.Itoa(k.Cost)
}

// waiting is what the row says past the columns: whether it is a draft, its
// labels, and the title.
func waiting(p *inbox.PullRequest) string {
	var b strings.Builder

	if p.Draft {
		b.WriteString("draft  ")
	}

	if len(p.Labels) > 0 {
		b.WriteString("[" + strings.Join(p.Labels, " ") + "]  ")
	}

	b.WriteString(p.Title)

	return b.String()
}

func (s *inboxScreen) act(a tui.Action, row *tui.Row) (string, bool, error) {
	// A bucket that failed carries one row standing for the failure, which has
	// no pull request behind it to act on.
	if row.Key == "" {
		return "", false, fmt.Errorf("%w: %s", errNoPullRequest, row.Tail)
	}

	at, err := parseRef(row.Key)
	if err != nil {
		return "", false, fmt.Errorf("%w: %s", errUnknownRow, row.Key)
	}

	switch a {
	case tui.ActChoose, tui.ActCheckout, tui.ActComment:
		s.next = &handoff{act: a, at: at}

		return leaving(a) + " " + row.Key, true, nil
	case tui.ActApprove:
		return s.approve(row.Key, at)
	case tui.ActBrowse:
		if err := ghrun.GH().Run(s.ctx, ".",
			"browse", "--repo", at.owner+"/"+at.repo, strconv.Itoa(at.number)); err != nil {
			return "", false, fmt.Errorf("opening %s: %w", row.Key, err)
		}

		return "opened " + row.Key, false, nil
	case tui.ActRefresh:
		return "", false, nil
	case tui.ActMark, tui.ActReply, tui.ActResolve, tui.ActDiscard:
		return "", false, errNotInInbox
	}

	return "", false, nil
}

// leaving says what the screen is closing for, since all three handoffs look
// the same from inside the frame.
func leaving(a tui.Action) string {
	switch a {
	case tui.ActCheckout:
		return "checking out"
	case tui.ActComment:
		return "commenting on"
	case tui.ActChoose, tui.ActMark, tui.ActBrowse, tui.ActReply, tui.ActResolve,
		tui.ActRefresh, tui.ActApprove, tui.ActDiscard:
	}

	return "opening"
}

// approve takes the key twice. An approval is the one thing this screen sends
// that cannot be undone by deleting something, and it is a claim about a diff
// that a queue row does not show.
func (s *inboxScreen) approve(key string, at ref) (string, bool, error) {
	if s.armed != key {
		s.armed = key

		return "A again to approve " + key, false, nil
	}

	s.armed = ""

	err := ghrun.GH().Run(s.ctx, ".", "pr", "review", strconv.Itoa(at.number),
		"--repo", at.owner+"/"+at.repo, "--approve")
	if err != nil {
		return "", false, fmt.Errorf("approving %s: %w", key, err)
	}

	return "approved " + key, false, nil
}
