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

// The queue is read by scanning a column, so the columns have to line up. They
// are measured per bucket: aligning across all three would let the widest
// repository name anywhere set the indent everywhere.
func TestWriteAlignsWithinABucket(t *testing.T) {
	t.Parallel()

	now := at("2026-09-01T12:00:00Z")
	buckets := []inbox.Bucket{{
		Name: "pending your review",
		Items: []inbox.PullRequest{
			{Repository: "a/b", Number: 7, Author: "someone", Title: "short", Updated: now.Add(-90 * time.Minute)},
			{
				Repository: "much/longer-repo", Number: 14691, Author: "a-very-long-username",
				Title: "long", Updated: now.Add(-3 * time.Hour), Draft: true,
			},
		},
	}}

	var b strings.Builder
	if err := inbox.Write(&b, buckets, now); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("wrote %d lines, want a heading and two items:\n%s", len(lines), b.String())
	}

	if !strings.HasPrefix(lines[0], "pending your review (2)") {
		t.Errorf("the heading does not carry the count: %q", lines[0])
	}

	// The author column starts at the same place on both lines, which is the
	// whole of what "skimmable" means here.
	first := strings.Index(lines[1], "someone")
	second := strings.Index(lines[2], "a-very-long-u")

	if first != second {
		t.Errorf("the author column starts at %d and %d:\n%s\n%s", first, second, lines[1], lines[2])
	}

	if !strings.Contains(lines[2], "draft") {
		t.Error("a draft is not marked")
	}

	if !strings.Contains(lines[1], "1h") || !strings.Contains(lines[2], "3h") {
		t.Errorf("staleness is missing:\n%s\n%s", lines[1], lines[2])
	}
}

// One search failing takes its own bucket down and not the queue, and the
// reason is worth one line rather than GitHub's four hundred characters.
func TestWriteKeepsGoingPastAFailedBucket(t *testing.T) {
	t.Parallel()

	now := at("2026-09-01T12:00:00Z")
	buckets := []inbox.Bucket{
		{Name: "pending your review", Err: "HTTP 403: You have exceeded a secondary rate limit. " +
			"Please wait a few minutes. For more on scraping GitHub see the terms of service."},
		{Name: "reviewed, merged", Items: []inbox.PullRequest{
			{Repository: "a/b", Number: 1, Author: "someone", Title: "landed", Updated: now.Add(-48 * time.Hour)},
		}},
	}

	var b strings.Builder
	if err := inbox.Write(&b, buckets, now); err != nil {
		t.Fatal(err)
	}

	out := b.String()
	if !strings.Contains(out, "secondary rate limit.") {
		t.Errorf("the reason is missing:\n%s", out)
	}

	if strings.Contains(out, "terms of service") {
		t.Errorf("the whole of GitHub's message was printed:\n%s", out)
	}

	if !strings.Contains(out, "landed") {
		t.Errorf("a later bucket was dropped because an earlier one failed:\n%s", out)
	}

	if !strings.Contains(out, "2d") {
		t.Errorf("staleness past a day is not in days:\n%s", out)
	}
}
