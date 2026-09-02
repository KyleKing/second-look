package inbox_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/inbox"
)

// Recency is right for thirteen rows and arbitrary for eighty, so the order is
// what triage actually wants: what you have started, then what is small, then
// what has waited longest, with drafts under all of it.
func TestRank(t *testing.T) {
	t.Parallel()

	at := func(days int) time.Time {
		return time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -days)
	}

	items := []inbox.PullRequest{
		{Repository: "o/fresh", Number: 1, Updated: at(1)},
		{Repository: "o/stale", Number: 2, Updated: at(9)},
		{Repository: "o/draft", Number: 3, Updated: at(30), Draft: true},
		{Repository: "o/big", Number: 4, Updated: at(2)},
		{Repository: "o/small", Number: 5, Updated: at(3)},
	}

	known := map[int]inbox.Known{
		4: {Reviewed: true, Cost: 70, Rated: true},
		5: {Reviewed: true, Cost: 9, Rated: true},
	}

	inbox.Rank(items, func(p *inbox.PullRequest) inbox.Known { return known[p.Number] })

	want := []string{"o/small", "o/big", "o/stale", "o/fresh", "o/draft"}

	for i, name := range want {
		if items[i].Repository != name {
			t.Errorf("row %d is %s, want %s (order: %s)", i, items[i].Repository, name, names(items))

			break
		}
	}
}

func names(items []inbox.PullRequest) string {
	var out strings.Builder
	for i := range items {
		out.WriteString(items[i].Repository + " ")
	}

	return out.String()
}

// A rating is about the diff at one moment. The update time is what says so,
// and a row pushed to since is one the number would mislead about, so ordering
// it by age beats ordering it by a rating of a diff nobody can see any more.
//
// What was asked is the other half: a diff no grammar answered for is recorded
// as read, so the queue does not fetch it again on every open.
func TestRecallTakesOnlyTheRatingsStillAboutTheseRows(t *testing.T) {
	t.Parallel()

	was := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	now := was.AddDate(0, 0, 1)

	items := []inbox.PullRequest{
		{Repository: "o/still", Number: 1, Updated: was},
		{Repository: "o/pushed", Number: 2, Updated: now},
		{Repository: "o/unseen", Number: 3, Updated: was},
		{Repository: "o/staged", Number: 4, Updated: was},
		{Repository: "o/yaml", Number: 5, Updated: was},
	}

	ratings := artifact.Ratings{
		"o/still#1":  {Updated: was, Cost: 12, Rated: true},
		"o/pushed#2": {Updated: was, Cost: 70, Rated: true},
		"o/staged#4": {Updated: was, Cost: 33, Rated: true},
		"o/yaml#5":   {Updated: was},
	}

	// A staged review's own rating is read from the head it was staged at, so
	// the queue's cache does not overwrite it.
	known := map[string]inbox.Known{"o/staged#4": {Reviewed: true, Cost: 5, Rated: true}}

	asked := inbox.Recall(items, ratings, known)

	for _, tc := range []struct {
		key   string
		cost  int
		rated bool
		ask   bool
	}{
		{key: "o/still#1", cost: 12, rated: true, ask: true},
		{key: "o/pushed#2"},
		{key: "o/unseen#3"},
		{key: "o/staged#4", cost: 5, rated: true, ask: true},
		{key: "o/yaml#5", ask: true},
	} {
		got := known[tc.key]
		if got.Rated != tc.rated || got.Cost != tc.cost {
			t.Errorf("%s is %+v, want cost %d rated %v", tc.key, got, tc.cost, tc.rated)
		}

		if asked[tc.key] != tc.ask {
			t.Errorf("%s was asked %v, want %v", tc.key, asked[tc.key], tc.ask)
		}
	}
}
