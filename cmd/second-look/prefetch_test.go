package main

import (
	"testing"
	"time"

	"github.com/kyleking/aragonite/forge/github"

	"github.com/kyleking/second-look/internal/inbox"
)

func queued(repo string, numbers ...int) inbox.Bucket {
	var b inbox.Bucket

	for _, n := range numbers {
		b.Items = append(b.Items, inbox.PullRequest{
			Repository: repo, Number: n, Title: "a change", Updated: time.Now(),
		})
	}

	return b
}

// The queue stages the next few rows in the order it is read, skips what this
// laptop already holds, and never asks for the same row twice.
func TestWhatIsStagedAhead(t *testing.T) {
	t.Parallel()

	s := &inboxScreen{
		ahead:   2,
		fetched: map[string]bool{},
		local:   map[string]inbox.Known{keyOf(&inbox.PullRequest{Repository: "acme/a", Number: 1}): {}},
		buckets: []inbox.Bucket{queued("acme/a", 1, 2, 3), queued("acme/b", 9)},
	}

	got := s.upcoming()
	if len(got) != 2 || got[0].Number != 2 || got[1].Number != 3 {
		t.Fatalf("staged %+v, want #2 and #3: a review already held is not fetched again", got)
	}

	if again := s.upcoming(); len(again) != 1 || again[0].Number != 9 {
		t.Errorf("a second pass asked for %+v, want only what it has not asked for", again)
	}
}

// A thin allowance holds the prefetch back, because ordering the queue and
// opening a review are reads too and this is the wrong half to spend the last
// of it on.
func TestPrefetchWaitsOutAThinAllowance(t *testing.T) {
	t.Parallel()

	s := &inboxScreen{ahead: 3, fetched: map[string]bool{}}

	s.budget = &github.Allowance{Limit: 5000, Remaining: 10}
	if s.canAfford() {
		t.Error("staged ahead with ten reads left")
	}

	s.budget = &github.Allowance{Limit: 5000, Remaining: 4000}
	if !s.canAfford() {
		t.Error("refused to stage ahead with four thousand reads left")
	}

	// A read that failed answers -1, and a queue is not left unprepared over an
	// allowance nobody could read.
	s.budget = &github.Allowance{Limit: -1, Remaining: -1}
	if !s.canAfford() {
		t.Error("an unreadable allowance stopped the prefetch")
	}
}

// Nothing is staged where the config turns it off.
func TestPrefetchOffStagesNothing(t *testing.T) {
	t.Parallel()

	s := &inboxScreen{ahead: 0, fetched: map[string]bool{}, buckets: []inbox.Bucket{queued("acme/a", 1)}}

	if cmd := s.prefetch(); cmd != nil {
		t.Error("prefetch = 0 staged something anyway")
	}
}
