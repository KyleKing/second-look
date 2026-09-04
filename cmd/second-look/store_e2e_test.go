package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
)

// One review, wherever it is read from. A review staged in one clone is moved
// into the store on the first read and answers from a second clone of the same
// repository, which is what makes an incremental re-review possible across
// directories.
func TestAReviewIsOneReviewAcrossClones(t *testing.T) {
	t.Parallel()

	first, sha := scratchRepo(t, headBranch)
	second, _ := scratchRepo(t, headBranch)

	// The old shape: staged in the working copy rather than in the store.
	body, err := os.ReadFile(filepath.Join("testdata", "review", "staged.toml"))
	if err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(first, ".second-look", "pr-2.toml")
	write(t, legacy, []byte(strings.ReplaceAll(string(body), fixtureHeadSHA, sha)))

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "one-store", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	if res := runCLI(t, s, first, "show", "2"); res.code != 0 {
		t.Fatalf("show in the first clone failed: %s%s", res.stdout, res.stderr)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("the review is still in the working copy: %v", err)
	}

	if _, err := os.Stat(filepath.Join(first, ".second-look", artifact.Pointer)); err != nil {
		t.Errorf("nothing in the clone says where the review went: %v", err)
	}

	res := runCLI(t, s, second, "show", "2")
	if res.code != 0 {
		t.Fatalf("show in the second clone failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, `"head_sha": "`+sha) {
		t.Errorf("the second clone read a different review:\n%s", res.stdout)
	}
}

// A bare number outside a checkout still names a review, because the store
// holds every one of them and only one answers to it.
func TestABareNumberResolvesFromTheStore(t *testing.T) {
	t.Parallel()

	dir := workspace(t, "staged.toml")

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "bare-number", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	res := runCLI(t, s, dir, "show", "2")
	if res.code != 0 {
		t.Fatalf("show failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, `"number": 2`) {
		t.Errorf("a bare number outside a checkout read nothing:\n%s", res.stdout)
	}
}

// What the agent reads: the diff with the anchors on it, and one comment with
// the hunk and the note around it. `show` alone hands over a path and a line
// number, which is not what the person is looking at.
func TestTheAgentReadsTheDiffAndOneCommentInContext(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)
	seedDiffAt(t, dir, sha)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "agent-reads", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	res := runCLI(t, s, dir, "show", "2", "--diff")
	if res.code != 0 {
		t.Fatalf("show --diff failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{"testdata/fixture/sample.go", "<<< unwrapped-read-error"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("%q is missing from the marked diff:\n%s", want, res.stdout)
		}
	}

	res = runCLI(t, s, dir, "context", "2", "unwrapped-read-error")
	if res.code != 0 {
		t.Fatalf("context failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{"NOTE (never posted)", ">>", "return 0, err"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("%q is missing from the comment's context:\n%s", want, res.stdout)
		}
	}

	if res := runCLI(t, s, dir, "context", "2", "no-such-comment"); res.code == 0 {
		t.Error("an unknown comment id was accepted")
	}
}

// The agent loop: a comment handed back is written out with its context, the
// agent answers it as a turn, and the answer comes back as a draft for the
// author to rule on. Posting refuses while any of it is outstanding.
func TestWorkHandedToAnAgentComesBackAsATurn(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)
	seedDiffAt(t, dir, sha)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "agent-loop", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	path := artifact.Path(stored(t, dir), 2)

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	r.Find("unwrapped-read-error").Status = artifact.StatusTodo

	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	res := runCLI(t, s, dir, "todo", "2")
	if res.code != 0 {
		t.Fatalf("todo failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{"1 comment(s) waiting on you", "unwrapped-read-error", "return 0, err"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("%q is missing from the todo set:\n%s", want, res.stdout)
		}
	}

	batch := `{"comments":[{"id":"unwrapped-read-error","path":"testdata/fixture/sample.go",` +
		`"line":19,"side":"RIGHT","body":"Wrap it with the path.","severity":"major",` +
		`"status":"ready","turn":[{"author":"claude","body":"Rewrote it to name the file."}]}]}`

	res = runCLIStdin(t, s, dir, batch, "comment", "add", "2")
	if res.code != 0 {
		t.Fatalf("comment add failed: %s%s", res.stdout, res.stderr)
	}

	back, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	c := back.Find("unwrapped-read-error")

	if c.Status != artifact.StatusDraft {
		t.Errorf("the agent's answer is %q, want a draft for the author to rule on", c.Status)
	}

	if len(c.Turns) != 1 || c.Turns[0].Author != "claude" {
		t.Errorf("the turn did not land: %+v", c.Turns)
	}

	// A second answer appends rather than replacing, so the exchange is kept.
	res = runCLIStdin(t, s, dir, batch, "comment", "add", "2")
	if res.code != 0 {
		t.Fatalf("the second comment add failed: %s%s", res.stdout, res.stderr)
	}

	if back, err = artifact.Load(path); err != nil {
		t.Fatal(err)
	}

	if got := len(back.Find("unwrapped-read-error").Turns); got != 2 {
		t.Errorf("%d turn(s) after two answers, want both kept", got)
	}
}

// Posting refuses while an agent still owes work, for the same reason it
// refuses a draft: it is unfinished.
func TestPostRefusesWhileWorkIsOutstanding(t *testing.T) {
	t.Parallel()

	dir := workspace(t, "triaged.toml")
	path := artifact.Path(stored(t, dir), 2)

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	r.Comments[0].Status = artifact.StatusTodo

	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	s := ghcassette.Replay(t, derive(t, "todo-blocks", guardOnly))

	res := runCLI(t, s, dir, "post", "2")
	if res.code == 0 {
		t.Fatalf("a review with work outstanding posted:\n%s", res.stdout)
	}

	if !strings.Contains(res.stderr, "still todo") {
		t.Errorf("the refusal did not name the reason: %s", res.stderr)
	}
}
