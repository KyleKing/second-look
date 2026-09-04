package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kyleking/aragonite/forge/github"

	"github.com/kyleking/second-look/internal/artifact"
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

// A row that merged, or that somebody else reviewed, leaves the queue while the
// review this session staged ahead of it stays on disk, invisible until the
// staged list is opened weeks later. So the queue prunes its own guesses.
//
// What it must never do is take work with them, which is why a review carrying
// anything a person could have written is left however stale it is.
func TestPruningTakesTheQueuesOwnGuessesAndNothingElse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root, err := artifact.StateHome()
	if err != nil {
		t.Fatal(err)
	}

	store := filepath.Join(root, "github.com", "acme", "a")
	stageFile(t, store, 1, nil)
	stageFile(t, store, 2, []artifact.Comment{{
		ID: "c1", Path: "a.go", Side: artifact.SideRight, Line: 1,
		Body: "a finding", Severity: "major", Status: artifact.StatusReady,
	}})

	s := &inboxScreen{
		ready: 2,
		fetched: map[string]bool{
			artifact.RatingKey("acme/a", 1): true,
			artifact.RatingKey("acme/a", 2): true,
		},
		buckets: []inbox.Bucket{queued("acme/a", 3)},
	}

	s.prunePrefetched()

	if _, err := os.Stat(artifact.Path(store, 1)); !os.IsNotExist(err) {
		t.Error("an empty review the queue no longer holds was kept")
	}

	if _, err := os.Stat(artifact.Path(store, 2)); err != nil {
		t.Errorf("a review carrying a staged comment was discarded: %v", err)
	}
}

func stageFile(t *testing.T, store string, number int, cs []artifact.Comment) {
	t.Helper()

	if err := artifact.Save(artifact.Path(store, number), &artifact.Review{
		Version: artifact.SchemaVersion, Host: "github.com", Owner: "acme", Repo: "a",
		Number: number, HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: cs,
	}); err != nil {
		t.Fatal(err)
	}
}
