package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/kyleking/second-look/internal/config"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/tui"
)

// The three queues, in the order the strip shows them and the digits pick them.
// The inbox comes first because it is what to read when nothing else is
// pressing, and what is staged here comes last because it is work already
// started rather than work to find.
const (
	tabInbox = iota
	tabConversations
	tabReviews
)

// openQueue is the queue screen: three tabs over one cursor, one filter
// vocabulary, and one set of movement keys.
//
// They were three programs, which meant three commands to remember and no way
// to go from a pull request waiting on you to the conversation on it without
// leaving and starting again. Each tab loads when it is first looked at, so
// opening on one costs what that one costs.
func openQueue(ctx context.Context, at int, stdin io.Reader, stdout io.Writer) error {
	for {
		next, err := queueOnce(ctx, at, stdin, stdout)
		if err != nil || next < 0 {
			return err
		}

		at = next
	}
}

// queueOnce draws the screen and performs whatever it was left for. It returns
// the tab to come back to, or -1 when the session is over: reviewing, replying,
// and opening a staged review all end in the review screen, which is where the
// rest of the work happens.
func queueOnce(ctx context.Context, at int, stdin io.Reader, stdout io.Writer) (int, error) {
	cfg, err := configured(os.Stderr)
	if err != nil {
		return -1, err
	}

	in := &inboxScreen{
		ctx:        ctx,
		configured: len(cfg.Sections) > 0,
		ahead:      keepAhead(cfg),
		plan:       func() []inbox.Bucket { return planQueue(cfg) },
	}

	th, err := newThreadsScreen(ctx)
	if err != nil {
		return -1, err
	}

	rows, err := staged()
	if err != nil {
		return -1, err
	}

	rv := &reviewsScreen{ctx: ctx, rows: rows}

	list := tui.NewTabs([]tui.Tab{
		{
			Name: "inbox", Title: "second-look inbox",
			Sections: in.sections, Act: in.act, Subtitle: in.counts,
			Hints: inboxHints, Help: inboxHelp, Loader: in,
		},
		{
			Name: "conversations", Title: "second-look conversations",
			Sections: th.sections, Act: th.act, Subtitle: th.counts,
			Hints: threadsHints, Help: threadsHelp, Loader: th,
		},
		{
			Name: "staged", Title: "second-look staged reviews",
			Sections: rv.sections, Act: rv.act, Subtitle: rv.counts,
			Hints: reviewsHints, Help: reviewsHelp,
		},
	}, at)

	_, runErr := tui.RunList(list)

	// The marks are worth keeping even when an action failed: what was read was
	// still read.
	if err := th.save(); err != nil {
		return -1, err
	}

	if runErr != nil {
		return -1, fmt.Errorf("reading your queue: %w", runErr)
	}

	return afterQueue(ctx, list.Tab(), in, th, rv, stdin, stdout)
}

// keepAhead is how many reviews the queue stages in front of the cursor. An
// unset config gets the built-in number and a zero turns it off.
func keepAhead(cfg *config.Config) int {
	if cfg.Prefetch == nil {
		return howManyAhead
	}

	return max(0, *cfg.Prefetch)
}

// afterQueue runs whatever the screen closed for, then says which tab to come
// back to. Only one of the four can be set: the screen quits on the action that
// sets it.
//
// Reading a review comes back to the queue rather than ending the session,
// because twenty-five reviews is one sitting: quitting the program to get to
// the next row makes the queue a list you consult rather than one you work
// through.
func afterQueue(
	ctx context.Context, at int, in *inboxScreen, th *threadsScreen, rv *reviewsScreen,
	stdin io.Reader, stdout io.Writer,
) (int, error) {
	switch {
	case rv.open != nil:
		return at, openRef(ctx, *rv.open, stdin, stdout)
	case th.reply != nil:
		return at, answer(ctx, th.reply, th.repo, stdin, stdout)
	case in.next == nil:
		return -1, nil
	case in.next.act == tui.ActChoose:
		return at, openRef(ctx, in.next.at, stdin, stdout)
	}

	// A checkout that could not move or an editor closed empty is not a reason
	// to lose the queue, so the failure is reported and the screen comes back.
	if err := perform(ctx, in.next, stdin, stdout); err != nil {
		return -1, err
	}

	return at, nil
}
