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

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
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
		tui.ActRefresh, tui.ActApprove:
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
	s.buckets = s.plan()
	s.waiting = len(s.buckets)
	s.armed = ""

	cmds := make([]tea.Cmd, 0, len(s.buckets))

	for i := range s.buckets {
		at, want := i, s.buckets[i]

		cmds = append(cmds, func() tea.Msg {
			return bucketMsg{at: at, bucket: inbox.Run(s.ctx, ".", want)}
		})
	}

	return tea.Batch(cmds...)
}

// Absorb takes a search that has answered. It runs on the program's own loop,
// so this is the one place the buckets are written.
func (s *inboxScreen) Absorb(msg tea.Msg) (tea.Cmd, bool) {
	answered, ok := msg.(bucketMsg)
	if !ok {
		return nil, false
	}

	if answered.at < len(s.buckets) {
		inbox.Rank(answered.bucket.Items, s.known)
		s.buckets[answered.at] = answered.bucket
		s.waiting--
	}

	return nil, true
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

		cost, rated := artifact.LoadScore(filepath.Dir(filepath.Dir(r.Path)), r.HeadSHA)
		out[fmt.Sprintf("%s#%d", r.Repository, r.Number)] = inbox.Known{
			Reviewed: true, Cost: cost, Rated: rated,
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
				Tail: rated(s.local[key]) + waiting(p),
			})
		}

		out = append(out, tui.Section{Name: b.Name, Rows: rows})
	}

	return out
}

const authorCap = 14

// rated is what an earlier read of this pull request made of it, which is the
// one number here that is about the change rather than about the queue.
func rated(k inbox.Known) string {
	if !k.Rated {
		return ""
	}

	return fmt.Sprintf("cost %d  ", k.Cost)
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
			"browse", "--repo", at.owner+"/"+at.repo, "-n", strconv.Itoa(at.number)); err != nil {
			return "", false, fmt.Errorf("opening %s: %w", row.Key, err)
		}

		return "opened " + row.Key, false, nil
	case tui.ActRefresh:
		return "", false, nil
	case tui.ActMark, tui.ActReply, tui.ActResolve:
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
		tui.ActRefresh, tui.ActApprove:
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
