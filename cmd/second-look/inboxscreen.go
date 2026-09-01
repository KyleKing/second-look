package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

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
	// reload runs the same searches again, closed over the config read before
	// the screen opened: a config error has nowhere to be printed while the
	// alternate screen is up.
	reload func() []inbox.Bucket
	// next is what the screen was left to do. Reviewing, checking out, and
	// writing a comment all need the terminal this screen owns, so each is
	// carried out and performed once it has closed.
	next *handoff
	// configured says the sections came from the config, which is what decides
	// whether the first one can be called what is waiting on you.
	configured bool
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

var inboxHelp = []string{
	helpMove,
	helpGroup,
	"  enter                open the review screen for it",
	"  C                    move a checkout onto it, asking before it stashes",
	"  m                    comment on the pull request itself, in $EDITOR",
	"  A                    approve it, A again to confirm",
	"  o                    open it on GitHub",
	"  ctrl+r               run the searches again",
	helpLeave,
	"",
	"  The buckets are the sections your config names, or the three built-in ones:",
	"  waiting on you, then what you answered and is still open, then what merged.",
	"  Opening one needs no checkout, and C is what gets one when this laptop has a",
	"  clone. Merging is not here: it is M in the review screen, after reading it.",
	"  A bucket whose search failed says so and leaves the others alone.",
}

// openInbox shows the queue and performs whatever the screen was left for,
// coming back to it afterwards.
//
// Reviewing is the one handoff that does not come back: the review screen is
// where the rest of the work happens, and returning to a queue behind it would
// re-run three searches nobody asked for.
func openInbox(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	for {
		cfg, err := configured(os.Stderr)
		if err != nil {
			return err
		}

		s := &inboxScreen{
			ctx:        ctx,
			configured: len(cfg.Sections) > 0,
			reload:     func() []inbox.Bucket { return runQueue(ctx, cfg) },
		}
		s.buckets = s.reload()

		list := tui.NewList("second-look inbox", s.sections, s.act).
			WithSubtitle(s.counts).
			WithHints(inboxHints).
			WithHelp(inboxHelp)

		if _, err := tui.RunList(list); err != nil {
			return fmt.Errorf("reading your review queue: %w", err)
		}

		if s.next == nil {
			return nil
		}

		if s.next.act == tui.ActChoose {
			return openRef(ctx, s.next.at, stdin, stdout)
		}

		if err := perform(ctx, s.next, stdin, stdout); err != nil {
			return err
		}
	}
}

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

// counts leads with the number worth acting on, which is what is waiting on you
// for the built-in buckets and the whole queue for sections somebody wrote: a
// configured first section is whatever its query asked for and calling it work
// waiting on you would be a guess.
//
// A failed search is named rather than left to make a short queue look quiet.
func (s *inboxScreen) counts() string {
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
		out = fmt.Sprintf("%d in %d section(s)", rows, len(s.buckets))
	}

	if failed == 0 {
		return out
	}

	return fmt.Sprintf("%s · %d search(es) failed", out, failed)
}

func (s *inboxScreen) sections() []tui.Section {
	now := time.Now()

	out := make([]tui.Section, 0, len(s.buckets))

	for i := range s.buckets {
		b := &s.buckets[i]

		if b.Err != "" {
			out = append(out, tui.Section{Name: b.Name, Rows: []tui.Row{{
				Left: "could not be read", Tail: humanize.FirstLine(b.Err),
			}}})

			continue
		}

		rows := make([]tui.Row, 0, len(b.Items))

		for j := range b.Items {
			p := &b.Items[j]
			rows = append(rows, tui.Row{
				Key:  fmt.Sprintf("%s#%d", p.Repository, p.Number),
				Left: fmt.Sprintf("%s#%d", p.Repository, p.Number),
				Mid:  humanize.Clip(p.Author, authorCap),
				Age:  humanize.Ago(p.Updated, now),
				Tail: waiting(p),
			})
		}

		out = append(out, tui.Section{Name: b.Name, Rows: rows})
	}

	return out
}

const authorCap = 14

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
		s.buckets = s.reload()
		s.armed = ""

		return s.counts(), false, nil
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
