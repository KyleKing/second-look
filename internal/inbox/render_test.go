package inbox_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/inbox"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}

	return t
}

// TestWrite pins the plain queue. Columns are measured per bucket, since
// aligning across buckets would let the widest repository name anywhere set the
// indent everywhere, and an author is clipped so one long name cannot push the
// rest off the line. One search failing takes its own bucket down and not the
// queue, and its reason is worth one sentence rather than GitHub's four hundred
// characters.
func TestWrite(t *testing.T) {
	t.Parallel()

	now := at("2026-09-01T12:00:00Z")
	buckets := []inbox.Bucket{
		{Name: "pending your review", Items: []inbox.PullRequest{
			{
				Repository: "a/b", Number: 7, Author: "someone", Title: "short", Updated: now.Add(-90 * time.Minute),
				Known: inbox.Known{
					Judgement: inbox.Judgement{Who: "you alone", Why: "asked", Priority: true, Area: true},
					Stack:     inbox.Stack{Position: 1, Size: 3},
				},
			},
			{
				Repository: "much/longer-repo", Number: 14691, Author: "a-very-long-username",
				Title: "long", Updated: now.Add(-3 * time.Hour), Draft: true,
			},
		}},
		{Name: "reviewed, open", Err: "HTTP 403: You have exceeded a secondary rate limit. " +
			"Please wait a few minutes. For more on scraping GitHub see the terms of service."},
		{Name: "authored, open", Err: "gh: not logged in"},
		{Name: "reviewed, merged", Items: []inbox.PullRequest{
			{Repository: "a/b", Number: 1, Author: "someone", Title: "landed", Updated: now.Add(-48 * time.Hour)},
		}},
	}

	want := `pending your review (2)
  a/b#7                   someone            1h  you alone  asked  priority  same area  stack 1/3  short
  much/longer-repo#14691  a-very-long-u…     3h  draft  long

reviewed, open (0)
  could not be read: HTTP 403: You have exceeded a secondary rate limit. (--json has the rest)

authored, open (0)
  could not be read: gh: not logged in

reviewed, merged (1)
  a/b#1  someone     2d  landed
`

	var b strings.Builder
	if err := inbox.Write(&b, buckets, now); err != nil {
		t.Fatal(err)
	}

	if got := b.String(); got != want {
		t.Errorf("wrote\n%s\nwant\n%s", got, want)
	}
}
