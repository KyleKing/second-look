package inbox_test

import (
	"strings"
	"testing"
	"time"

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
