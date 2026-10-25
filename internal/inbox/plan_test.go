package inbox_test

import (
	"slices"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/inbox"
)

type splitCase struct {
	name    string
	rows    []inbox.PullRequest
	records map[string]inbox.Record
	reuse   []string
	fetch   []string
}

var (
	now     = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	updated = now.Add(-2 * time.Hour)
	recent  = now.Add(-time.Minute)
)

func row(id string, number int) inbox.PullRequest {
	return inbox.PullRequest{ID: id, Repository: "example/widgets", Number: number, Updated: updated}
}

func held(checks string, fetched time.Time) inbox.Record {
	return inbox.Record{Facts: inbox.Facts{Checks: checks}, Updated: updated, Fetched: fetched}
}

func splitCases() []splitCase {
	return []splitCase{
		{
			name:    "an unchanged row with finished checks is reused",
			rows:    []inbox.PullRequest{row("a", 1)},
			records: map[string]inbox.Record{"example/widgets#1": held("SUCCESS", recent)},
			reuse:   []string{"a"},
		},
		{
			name:  "a row nobody has read is fetched",
			rows:  []inbox.PullRequest{row("a", 1)},
			fetch: []string{"a"},
		},
		{
			name: "a row whose updatedAt moved is fetched",
			rows: []inbox.PullRequest{func() inbox.PullRequest {
				r := row("a", 1)
				r.Updated = now

				return r
			}()},
			records: map[string]inbox.Record{"example/widgets#1": held("SUCCESS", recent)},
			fetch:   []string{"a"},
		},
		{
			name:    "pending checks are fetched again",
			rows:    []inbox.PullRequest{row("a", 1)},
			records: map[string]inbox.Record{"example/widgets#1": held("PENDING", recent)},
			fetch:   []string{"a"},
		},
		{
			name:    "expected checks are fetched again",
			rows:    []inbox.PullRequest{row("a", 1)},
			records: map[string]inbox.Record{"example/widgets#1": held("EXPECTED", recent)},
			fetch:   []string{"a"},
		},
		{
			name:    "a row with no checks is not running any",
			rows:    []inbox.PullRequest{row("a", 1)},
			records: map[string]inbox.Record{"example/widgets#1": held("", recent)},
			reuse:   []string{"a"},
		},
		{
			name:    "a record exactly as old as the limit still holds",
			rows:    []inbox.PullRequest{row("a", 1)},
			records: map[string]inbox.Record{"example/widgets#1": held("SUCCESS", now.Add(-inbox.MaxAge))},
			reuse:   []string{"a"},
		},
		{
			name:    "a record past the limit is fetched",
			rows:    []inbox.PullRequest{row("a", 1)},
			records: map[string]inbox.Record{"example/widgets#1": held("SUCCESS", now.Add(-inbox.MaxAge-time.Second))},
			fetch:   []string{"a"},
		},
		{
			name:    "a row with no node id cannot be asked about",
			rows:    []inbox.PullRequest{row("", 1)},
			records: nil,
			reuse:   []string{""},
		},
		{
			name:    "a row found by two searches is planned once",
			rows:    []inbox.PullRequest{row("a", 1), row("a", 1), row("b", 2)},
			records: map[string]inbox.Record{"example/widgets#2": held("SUCCESS", recent)},
			reuse:   []string{"b"},
			fetch:   []string{"a"},
		},
		{
			name:    "no records fetches every row, in the order the searches gave them",
			rows:    []inbox.PullRequest{row("b", 2), row("a", 1)},
			records: nil,
			fetch:   []string{"b", "a"},
		},
	}
}

func TestSplit(t *testing.T) {
	t.Parallel()

	for _, c := range splitCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			reuse, fetch := inbox.Split(c.records, c.rows, now)

			ids := func(rows []inbox.PullRequest) []string {
				out := make([]string, 0, len(rows))
				for i := range rows {
					out = append(out, rows[i].ID)
				}

				return out
			}

			if got := ids(reuse); !slices.Equal(got, append([]string{}, c.reuse...)) {
				t.Errorf("reuse is %q, want %q", got, c.reuse)
			}

			if got := ids(fetch); !slices.Equal(got, append([]string{}, c.fetch...)) {
				t.Errorf("fetch is %q, want %q", got, c.fetch)
			}
		})
	}
}
