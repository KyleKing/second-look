package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/prepared"
)

// howManyAhead is how far ahead of the cursor the queue prepares by default.
// Three is enough that the next review is ready by the time the current one is
// finished, and `prefetch` in config.toml is what changes it.
const howManyAhead = 3

// eachCosts is what staging one review spends of the hourly allowance: the pull
// request, its diff, and its conversations.
const eachCosts = 3

// errNotAnOwnerName reports a queue row whose repository is not owner/name,
// which no target can be built from.
var errNotAnOwnerName = errors.New("not an owner/name")

// prefetchedMsg is one review staged ahead of being asked for.
type prefetchedMsg struct {
	key string
	err error
}

// prefetch stages the next few reviews in the background, in the order the
// queue is read, so the one after this one is ready before this one is
// finished. It is what turns twenty-five reviews into a sitting rather than
// twenty-five waits.
//
// A row this laptop already holds a review for is skipped: what is on disk is
// what a re-review reads, and refetching it would throw away the read marks the
// staged review is pinned to.
func (s *inboxScreen) prefetch() tea.Cmd {
	if s.ahead == 0 || !s.canAfford() {
		return nil
	}

	want := s.upcoming()
	if len(want) == 0 {
		return nil
	}

	cmds := make([]tea.Cmd, 0, len(want))

	for i := range want {
		p := want[i]
		s.fetching++

		cmds = append(cmds, func() tea.Msg {
			s.slots <- struct{}{}
			defer func() { <-s.slots }()

			return prefetchedMsg{key: keyOf(&p), err: stageAhead(s.ctx, p)}
		})
	}

	return tea.Batch(cmds...)
}

// canAfford holds the prefetch back when the hourly allowance is thin. Ordering
// the queue and opening a review are reads too, and spending the last of the
// allowance on rows nobody has asked for is the wrong half to spend it on.
func (s *inboxScreen) canAfford() bool {
	if s.budget == nil || s.budget.Remaining < 0 {
		return true
	}

	return s.budget.Remaining-s.spent > s.ahead*eachCosts*2
}

// upcoming is the rows with nothing staged for them, in the order the queue is
// read, up to how many the config asks to keep ahead.
func (s *inboxScreen) upcoming() []inbox.PullRequest {
	var out []inbox.PullRequest

	for i := range s.buckets {
		items := s.buckets[i].Items

		for j := range items {
			if len(out) == s.ahead {
				return out
			}

			key := keyOf(&items[j])
			if _, held := s.local[key]; held || s.fetched[key] {
				continue
			}

			s.fetched[key] = true
			out = append(out, items[j])
		}
	}

	return out
}

// stage prepares one review with no checkout, which caches the diff and the open
// conversations and writes the artifact. A detached target moves no working
// copy, so nothing a prefetch does can be noticed in a clone.
func stageAhead(ctx context.Context, p inbox.PullRequest) error {
	owner, repo, ok := strings.Cut(p.Repository, "/")
	if !ok {
		return fmt.Errorf("%s: %w", p.Repository, errNotAnOwnerName)
	}

	t, err := get.Away(owner, repo, p.Number)
	if err != nil {
		//nolint:wrapcheck // Away's own error already names the offending part
		return err
	}

	//nolint:wrapcheck // Run's own error already names the pull request
	return get.Run(ctx, io.Discard, t)
}

// absorbPrefetch records one answer. A row that could not be staged is not
// worth a message of its own: the review opens the way it always did, one read
// later than it would have.
func (s *inboxScreen) absorbPrefetch(answered prefetchedMsg) {
	s.fetching--

	if answered.err == nil {
		s.ready++
	}
}

// readyWord is what the header says about the reviews waiting ahead of the
// cursor, and nothing at all while none are.
func (s *inboxScreen) readyWord() string {
	switch {
	case s.fetching > 0:
		return fmt.Sprintf(" · staging %d ahead", s.fetching)
	case s.ready > 0:
		return fmt.Sprintf(" · %d staged ahead", s.ready)
	}

	return ""
}

func keyOf(p *inbox.PullRequest) string {
	return artifact.RatingKey(p.Repository, p.Number)
}

// prunePrefetched discards the reviews this session staged ahead that the queue
// no longer holds, which is what a row that merged or was reviewed elsewhere
// leaves behind.
//
// Only what this session staged and nobody has written into goes. A review
// carrying a comment, a body, or a note is work however it got there, and
// losing it would be far worse than keeping a stale one.
func (s *inboxScreen) prunePrefetched() {
	if len(s.fetched) == 0 {
		return
	}

	rows, err := staged()
	if err != nil {
		return
	}

	holds := s.inTheQueue()

	for i := range rows {
		r := &rows[i]
		key := artifact.RatingKey(r.Repository, r.Number)

		if !s.fetched[key] || holds[key] || r.Total() > 0 || r.Body || r.Broken != "" {
			continue
		}

		if err := prepared.Discard(r); err == nil {
			delete(s.fetched, key)

			s.ready = max(0, s.ready-1)
		}
	}
}

// inTheQueue is every pull request the searches answered with, which is what a
// prefetched review has to still be one of to be worth keeping.
func (s *inboxScreen) inTheQueue() map[string]bool {
	out := map[string]bool{}

	for i := range s.buckets {
		items := s.buckets[i].Items
		for j := range items {
			out[keyOf(&items[j])] = true
		}
	}

	return out
}
