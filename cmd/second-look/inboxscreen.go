package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/resolve"
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
	open    *ref
}

var inboxHints = [][2]string{
	{enterKey, "review"},
	{"o", "GitHub"},
	{"tab", "group"},
	{"ctrl+r", "refresh"},
	{"?", helpArg},
}

var inboxHelp = []string{
	helpMove,
	helpGroup,
	"  enter                open the review screen for it",
	"  o                    open it on GitHub",
	"  ctrl+r               run the searches again",
	helpLeave,
	"",
	"  The buckets are in the order they want doing: waiting on you, then what you",
	"  answered and is still open, then what has since merged.",
	"  Opening one needs no checkout. C inside the review screen moves a checkout",
	"  onto the pull request when this laptop has one.",
	"  A bucket whose search failed says so and leaves the others alone.",
}

// openInbox shows the queue and opens whichever pull request was chosen, once
// this screen has given the terminal back.
func openInbox(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	s := &inboxScreen{ctx: ctx, buckets: inbox.Buckets(ctx, ".", inboxLimit)}

	list := tui.NewList("second-look inbox", s.sections, s.act).
		WithSubtitle(s.counts).
		WithHints(inboxHints).
		WithHelp(inboxHelp)

	if _, err := tui.RunList(list); err != nil {
		return fmt.Errorf("reading your review queue: %w", err)
	}

	if s.open != nil {
		return openRef(ctx, *s.open, stdin, stdout)
	}

	return nil
}

// counts leads with what is waiting, since that is the only bucket with
// anything to do in it, and names how many searches failed rather than leaving
// a short queue looking like a quiet one.
func (s *inboxScreen) counts() string {
	waiting, failed := 0, 0

	for i := range s.buckets {
		if s.buckets[i].Err != "" {
			failed++

			continue
		}

		if i == 0 {
			waiting = len(s.buckets[i].Items)
		}
	}

	if failed == 0 {
		return fmt.Sprintf("%d waiting on you", waiting)
	}

	return fmt.Sprintf("%d waiting on you · %d search(es) failed", waiting, failed)
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
				// Nothing has been read in a queue that reads GitHub and no
				// local state, so the mark means what is still open and unmerged.
				Unread: i == 0,
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
	case tui.ActChoose:
		s.open = &at

		return "opening " + row.Key, true, nil
	case tui.ActBrowse:
		if err := resolve.GH().Run(s.ctx, ".",
			"browse", "--repo", at.owner+"/"+at.repo, "-n", strconv.Itoa(at.number)); err != nil {
			return "", false, fmt.Errorf("opening %s: %w", row.Key, err)
		}

		return "opened " + row.Key, false, nil
	case tui.ActRefresh:
		s.buckets = inbox.Buckets(s.ctx, ".", inboxLimit)

		return s.counts(), false, nil
	case tui.ActMark, tui.ActReply, tui.ActResolve:
		return "", false, errNotInInbox
	}

	return "", false, nil
}
