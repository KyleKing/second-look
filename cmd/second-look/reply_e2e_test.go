package main_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
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

// TestPostReportsAReplyThatFailedAfterTheReview is the reply failing after the
// review already submitted: the review is on GitHub for good, so the staged
// file keeps only the reply rather than pretending nothing posted.
func TestPostReportsAReplyThatFailedAfterTheReview(t *testing.T) {
	t.Parallel()

	cassette := deriveFrom(t, "post-reply", "reply-failed", func(c *ghcassette.Cassette) {
		// The reply is the last call, after the review's submit.
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

	if !strings.Contains(res.stderr, "the review posted as") {
		t.Errorf("want the posted review named, got %q", res.stderr)
	}
	if !strings.Contains(res.stderr, "only what did not go out") {
		t.Errorf("want the retry shape explained, got %q", res.stderr)
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatalf("the staged review that remains: %v", err)
	}
	for i := range review.Comments {
		if review.Comments[i].InReplyTo == 0 {
			t.Error("a comment the review already carried is still staged and would post twice")
		}
	}
}

// A won't-fix verdict posts its reply and then resolves the thread, in that
// order: the recorded reply is followed by the one call a resolve adds.
func TestPostResolvesTheThreadItAnswered(t *testing.T) {
	t.Parallel()

	const thread = "PRRT_kwDOT-RSFs6d8L5j"

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
