package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/forge/github"
	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/checkouts"
	"github.com/kyleking/second-look/internal/cost"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/lease"
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
	// fetching counts the reviews being staged ahead of the cursor, ready
	// counts the ones that landed, and fetched is every row already asked for,
	// so a refresh does not stage the same one twice.
	fetching int
	ready    int
	fetched  map[string]bool
	// ahead is how many reviews to keep staged in front of the cursor, from the
	// config.
	ahead int
	// short marks a run that left rows unrated because the hourly allowance
	// would not cover them, which is a different thing from a diff that could
	// not be read and wants saying differently.
	short bool
	// unread counts the rows whose diff could not be fetched, which is what a
	// rate limit or a dropped connection looks like. Saying so beats a queue
	// that quietly orders itself by age and looks like it never tried.
	unread int
	// order is which of inbox.Orders the queue is arranged by, cycled by s.
	order int
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
	// added and removed are how many lines the same read counted.
	added, removed int
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
	{"s", "sort"},
	{"?", helpArg},
}

var inboxHelp = helpFor(helpMove(), helpGroup(), [][2]string{
	{enterKey, "open the review screen for it"},
	{"C", "move a checkout onto it, asking before it stashes"},
	{"m", "comment on the pull request itself, in $EDITOR"},
	{"A", "approve it, A again to confirm"},
	{"o", "open it on GitHub"},
	{"s", "cycle to the next row order"},
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
		tui.ActRefresh, tui.ActApprove, tui.ActDiscard, tui.ActSort:
		return nil
	}

	if err == nil {
		return nil
	}

	return write(stdout, err.Error()+"\n")
}

// checkoutRef moves a working copy onto a pull request from the queue. The
// clone is picked from every checkout of the repository rather than assuming
// this directory is it, and leased for the sitting so a second second-look
// does not move the same tree.
func checkoutRef(ctx context.Context, at ref, stdin io.Reader, stdout io.Writer) error {
	repo := at.owner + "/" + at.repo
	if at.here() {
		repo = currentRepo(ctx)
	}

	owner, name, found := strings.Cut(repo, "/")
	if !found || owner == "" || name == "" {
		return fmt.Errorf("checking out %s: %w", at, errNoCheckoutHere)
	}

	head := at.head
	if head == "" {
		if pr, err := github.GetPR(ctx, ".", repo, at.number); err == nil {
			head = pr.HeadRef
		}
	}

	cands, err := clonesFor(ctx, checkouts.Dashboard(), repo, head)
	if err != nil {
		return fmt.Errorf("checking out %s: %w", at, err)
	}
	if len(cands) == 0 {
		return fmt.Errorf("%s: %w", repo, errNoCheckoutHere)
	}

	return claimFirst(ctx, repo, cands, at, stdin, stdout)
}

// claimFirst walks the ranked candidates and prepares the first one the sitting
// can hold: a clone leased to another live second-look is skipped, one that is
// not the repository at all is remembered for the error it leaves, and the
// first claim that takes is where the loop stops.
func claimFirst(
	ctx context.Context, repo string, cands []checkouts.Checkout, at ref, stdin io.Reader, stdout io.Writer,
) error {
	held := lease.List(get.Host, repo)
	mine := os.Getpid()

	var lastErr error

	for i := range cands {
		path := cands[i].Path
		if rec, ok := held[path]; ok && rec.PID != mine {
			continue
		}

		done, err := claimClone(ctx, repo, path, at, stdin, stdout)
		if err != nil {
			var taken *lease.TakenError
			switch {
			case errors.As(err, &taken):
				continue
			case errors.Is(err, errNotTheClone):
				lastErr = err
			default:
				return fmt.Errorf("checking out %s: %w", at, err)
			}

			continue
		}

		if done {
			return nil
		}
	}

	if lastErr != nil {
		return fmt.Errorf("checking out %s: %w", at, lastErr)
	}

	return fmt.Errorf("%s: %w", repo, errEveryCloneLeased)
}

// errNotTheClone marks a candidate that resolves to a detached or unresolvable
// target: the dashboard matched the remote id, but the clone's remotes do not
// name the repository, or the path is not a repo at all.
var errNotTheClone = errors.New("the clone does not belong to the repository")

// claimClone leases path for repo and prepares it for at, releasing a newly
// taken claim when the prepare fails. The boolean is true once the checkout
// moved, false while candidates remain.
func claimClone(
	ctx context.Context, repo, path string, at ref, stdin io.Reader, stdout io.Writer,
) (bool, error) {
	owner, name, _ := strings.Cut(repo, "/")

	t, err := get.Resolve(ctx, path, owner, name, at.number)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errNotTheClone, err)
	}
	if t.Detached() {
		return false, fmt.Errorf("%w: %s", errNotTheClone, path)
	}

	own := lease.Ours(get.Host, repo)

	h := own
	if own == nil || own.Record.Path != path {
		// One claim per repository is what the sitting keeps: taking a
		// second clone hands the first back rather than holding both.
		if own != nil {
			own.Release()
		}

		var taken *lease.TakenError

		h, err = lease.Acquire(get.Host, repo, path)
		switch {
		case errors.As(err, &taken):
			return false, taken
		case err != nil:
			return false, fmt.Errorf("leasing %s: %w", path, err)
		}
	}

	if err := get.Prepare(ctx, stdout, t, confirm(stdin, stdout)); err != nil {
		if h != own {
			h.Release()
		}

		return false, fmt.Errorf("preparing %s: %w", path, err)
	}

	if err := write(stdout, "leased "+path+" for "+repo+"\n"); err != nil {
		return false, err
	}

	return true, nil
}

// clonesFor is every checkout of repo C may move, ranked like the
// dashboard's but with two more entries where they apply: this directory when
// it is a clone the scan does not reach, and the sitting's own lease first,
// since it is already claimed.
func clonesFor(
	ctx context.Context, runner checkouts.Runner, repo, head string,
) ([]checkouts.Checkout, error) {
	found, findErr := checkouts.Find(ctx, runner, repo, head)

	if c, ok := cwdClone(ctx, repo); ok &&
		!slices.ContainsFunc(found, func(f checkouts.Checkout) bool { return f.Path == c.Path }) {
		found = append(found, c)
		checkouts.Rank(found, head)
	}

	if ours := lease.Ours(get.Host, repo); ours != nil {
		path := ours.Record.Path
		found = slices.DeleteFunc(found, func(f checkouts.Checkout) bool { return f.Path == path })
		found = append([]checkouts.Checkout{{Path: path}}, found...)
	}

	if len(found) == 0 && findErr != nil {
		return nil, fmt.Errorf("finding clones of %s: %w", repo, findErr)
	}

	return found, nil
}

// cwdClone is this directory as a checkout candidate when it is a clone of
// repo, which the dashboard's scan paths may not reach.
func cwdClone(ctx context.Context, repo string) (checkouts.Checkout, bool) {
	if !strings.EqualFold(currentRepo(ctx), repo) {
		return checkouts.Checkout{}, false
	}

	abs, err := filepath.Abs(".")
	if err != nil {
		return checkouts.Checkout{}, false
	}

	c := checkouts.Checkout{Path: abs}
	ops := vcs.GetOperations(abs)

	if b, err := ops.GetCurrentBranch(ctx, abs); err == nil {
		c.Branch = b
	}
	if s, err := ops.GetRepoSummary(ctx, abs); err == nil {
		c.Dirty = s.UncommittedCount() > 0
	}

	return c, true
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

	s.ratings = artifact.LoadRatings()
	s.asked = map[string]bool{}
	s.buckets = s.plan()
	s.waiting = len(s.buckets)
	s.armed = ""
	s.pending, s.listening, s.unread = 0, false, 0
	s.budget, s.queued, s.spent, s.short = nil, nil, 0, false
	s.fetching, s.ready, s.fetched = 0, 0, map[string]bool{}
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
	case prefetchedMsg:
		s.absorbPrefetch(answered)

		return nil, true
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

	s.arrange(answered.bucket.Items)

	s.buckets[answered.at] = answered.bucket
	s.waiting--

	rate := s.rate(answered.bucket.Items)

	// The queue is prepared once every search has answered, so what is staged
	// ahead is the order the queue is actually read in rather than whichever
	// search came back first.
	if s.waiting > 0 {
		return rate
	}

	s.prunePrefetched()

	return tea.Batch(rate, s.prefetch())
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
			Added: answered.added, Removed: answered.removed,
		}
	} else {
		s.unread++
	}

	if answered.read {
		known := s.local[answered.key]
		known.Added, known.Removed = answered.added, answered.removed

		if answered.rated {
			known.Cost, known.Rated = answered.cost, true
		}

		s.local[answered.key] = known
	}

	if answered.rated {
		for i := range s.buckets {
			s.arrange(s.buckets[i].Items)
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

// restedOn rates the row the cursor has stopped on.
//
// The burst leaves two kinds of row unrated: one in a bucket short enough to
// read at a glance, where a read per row buys nothing, and one the hourly
// allowance would not cover. Both are worth a read once somebody is actually
// looking at them, which bounds what an unrated queue costs to what is read
// rather than to what was searched for.
func (s *inboxScreen) restedOn(key string) tea.Cmd {
	if s.local[key].Rated || s.asked[key] || !structure.Available() {
		return nil
	}

	p, ok := s.rowFor(key)
	if !ok {
		return nil
	}

	s.queued = append(s.queued, p)

	return s.spend()
}

// rowFor is the pull request a row names, and false for a heading or for a row
// belonging to another tab.
func (s *inboxScreen) rowFor(key string) (inbox.PullRequest, bool) {
	for i := range s.buckets {
		items := s.buckets[i].Items
		for j := range items {
			if keyOf(&items[j]) == key {
				return items[j], true
			}
		}
	}

	return inbox.PullRequest{}, false
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

		s.asked[key] = true
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

	if score, size, err := cost.Of(s.ctx, ".", p.Repository, p.Number); err == nil {
		answer.read = true
		answer.cost, answer.rated = score.Total, score.Rated()
		answer.added, answer.removed = size.Added, size.Removed
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
//
// It is never nil, since Recall writes what it recovers into this map.
func localKnowledge() map[string]inbox.Known {
	rows, err := staged()
	if err != nil {
		rows = nil
	}

	out := make(map[string]inbox.Known, len(rows))

	for i := range rows {
		r := &rows[i]

		was, rated := artifact.LoadScore(filepath.Dir(filepath.Dir(r.Path)), r.HeadSHA)
		out[artifact.RatingKey(r.Repository, r.Number)] = inbox.Known{
			Reviewed: true, Cost: was.Total, Rated: rated,
			Added: was.Added, Removed: was.Removed,
			Ready: r.Ready, Draft: r.Draft, Replies: r.Replies,
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
		return humanize.Plural(s.unread, "row") + " could not be rated" + s.readyWord()
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

	out += s.readyWord()
	out += " · sorted by " + inbox.Orders[s.order].Name

	if failed == 0 {
		return out
	}

	return out + " · " + humanize.Plural(failed, "search", "searches") + " failed"
}

// note is what the tab strip says: how much is waiting on you, once the
// searches have answered. It counts the same rows counts does, so the strip
// and the header never disagree.
func (s *inboxScreen) note() string {
	if s.waiting > 0 {
		return ""
	}

	rows := 0
	for i := range s.buckets {
		if s.buckets[i].Err != "" {
			continue
		}

		if !s.configured && i > 0 {
			continue
		}

		rows += len(s.buckets[i].Items)
	}

	if rows == 0 {
		return ""
	}

	return fmt.Sprintf("%d waiting", rows)
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
				Key:     key,
				Left:    key,
				Repo:    p.Repository,
				Mid:     humanize.Clip(p.Author, authorCap),
				Age:     humanize.Ago(p.Updated, now),
				Cost:    rated(s.local[key]),
				Added:   added(s.local[key]),
				Removed: removed(s.local[key]),
				Tail:    holding(s.local[key]) + waiting(p),
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

// added and removed are how many lines the change touched, bare of any sign:
// the list screen draws that.
func added(k inbox.Known) string {
	if !measured(k) {
		return ""
	}

	return humanize.Count(k.Added)
}

func removed(k inbox.Known) string {
	if !measured(k) {
		return ""
	}

	return humanize.Count(k.Removed)
}

// measured tells a change nobody has read from one that changed no code, since
// a diff of nothing but a re-indent counts zero on both sides and is still
// something somebody looked at.
func measured(k inbox.Known) bool { return k.Added > 0 || k.Removed > 0 }

// holding is what a review staged here already carries, which is the queue's
// only sign that a row was started. The mark leads, because a row with work in
// it is the one to come back to rather than one to pick up cold.
func holding(k inbox.Known) string {
	if !k.Reviewed {
		return ""
	}

	parts := make([]string, 0, 3)

	for _, c := range []struct {
		n           int
		one, plural string
	}{
		{k.Ready, "ready", "ready"},
		{k.Draft, "draft", "drafts"},
		{k.Replies, "reply", "replies"},
	} {
		if c.n > 0 {
			parts = append(parts, humanize.Plural(c.n, c.one, c.plural))
		}
	}

	if len(parts) == 0 {
		return "● staged  "
	}

	return "● " + strings.Join(parts, " ") + "  "
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
	// Sorting is about the queue rather than the row the cursor is on, and a
	// bucket that failed still has one to cycle through.
	if a == tui.ActSort {
		return s.sort(), false, nil
	}

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
	case tui.ActSort:
		// ActSort needs no row, so it returns before this switch is reached.
	}

	return "", false, nil
}

// arrange puts one bucket's rows in whichever of inbox.Orders is current, so a
// bucket arriving after s was pressed lands in the order already chosen rather
// than back in triage order.
func (s *inboxScreen) arrange(items []inbox.PullRequest) {
	inbox.Orders[s.order].Order(items, s.known)
}

// sort advances to the next of inbox.Orders and puts every bucket back in it,
// so s cycles the whole queue rather than one section at a time.
func (s *inboxScreen) sort() string {
	s.order = (s.order + 1) % len(inbox.Orders)

	for i := range s.buckets {
		s.arrange(s.buckets[i].Items)
	}

	return "sorted by " + inbox.Orders[s.order].Name
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
		tui.ActRefresh, tui.ActApprove, tui.ActDiscard, tui.ActSort:
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
