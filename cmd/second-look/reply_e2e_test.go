package main_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/resolve"
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

// A won't-fix verdict posts its reply and then resolves the thread, in that
// order: the recorded reply is followed by the one call a resolve adds.
func TestPostResolvesTheThreadItAnswered(t *testing.T) {
	t.Parallel()

	const thread = "PRRT_kwDOT-RSFs6BudgetThread"

	cassette := deriveFrom(t, "post-reply", "reply-resolve", func(c *ghcassette.Cassette) {
		c.Interactions = append(c.Interactions, ghcassette.Interaction{
			Args:   resolve.ThreadArgs(thread),
			Stdout: `{"data":{"resolveReviewThread":{"thread":{"isResolved":true}}}}`,
		})
	})

	s := ghcassette.Replay(t, cassette)
	dir := workspace(t, "reply-resolve.toml")

	res := runCLI(t, s, dir, "post", "2")
	if res.code != 0 {
		t.Fatalf("post failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, "resolved thread "+thread) {
		t.Errorf("the thread was not reported resolved:\n%s", res.stdout)
	}

	s.RequireAllPlayed(t)
}

// A todo reply posts like any reply, and --fixes names who gets its fix
// afterwards: nobody by default, the written set for local.
func TestPostHandsATodoReplyToWhoeverTheFlagNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "nobody by default", want: "removed the staged review"},
		{name: "local writes the set", args: []string{"--fixes", "local"}, want: "1 todo written to"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := ghcassette.Replay(t, cassettePath(t, "post-reply"))

			res := runCLI(t, s, workspace(t, "reply-todo.toml"), append([]string{"post", "2"}, tc.args...)...)
			if res.code != 0 {
				t.Fatalf("post failed: %s%s", res.stdout, res.stderr)
			}

			if !strings.Contains(res.stdout, tc.want) {
				t.Errorf("stdout never said %q:\n%s", tc.want, res.stdout)
			}

			s.RequireAllPlayed(t)
		})
	}
}
