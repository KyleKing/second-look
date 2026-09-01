package main_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/ghcassette"
)

// TestPostReply replays the review that answers a thread the earlier recording
// created. A reply goes to its own endpoint rather than inside the review
// payload, so both requests are in the cassette and both have to happen.
func TestPostReply(t *testing.T) {
	t.Parallel()

	s := ghcassette.Start(t, cassettePath(t, "post-reply"))
	dir := workspace(t, "reply.toml")

	res := runCLI(t, s, dir, "post", "2")
	if res.code != 0 {
		t.Fatalf("post failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(res.stdout))
	s.RequireAllPlayed(t)
}

// TestPostReportsAReplyThatFailedAfterTheReview is the path that cannot be
// retried: the review is already on GitHub, so the message has to say so and
// the prepared review has to stay on disk rather than be removed as posted.
func TestPostReportsAReplyThatFailedAfterTheReview(t *testing.T) {
	t.Parallel()

	cassette := deriveFrom(t, "post-reply", "reply-failed", func(c *ghcassette.Cassette) {
		last := len(c.Interactions) - 1
		c.Interactions[last].Exit = 1
		c.Interactions[last].Stdout = ""
		c.Interactions[last].Stderr = "gh: Not Found (HTTP 404)\n"
	})

	s := ghcassette.Replay(t, cassette)
	dir := workspace(t, "reply.toml")

	res := runCLI(t, s, dir, "post", "2")
	if res.code == 0 {
		t.Fatal("expected a failed reply to fail the command")
	}

	if !strings.Contains(res.stderr, "would post the review twice") {
		t.Errorf("want the un-retryable state named, got %q", res.stderr)
	}

	if !strings.Contains(res.stdout, "posted /repos/KyleKing/second-look/pulls/2/reviews") {
		t.Errorf("want the review that did post reported, got %q", res.stdout)
	}
}
