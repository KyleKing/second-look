package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
)

// TestPostReview replays the review that was actually posted to
// KyleKing/second-look#2: the two guard reads, the review itself, and nothing
// for the comments held back. It is the one test that proves the whole path,
// binary included.
func TestPostReview(t *testing.T) {
	t.Parallel()

	s := ghcassette.Start(t, cassettePath(t, "post-review"))
	dir := workspace(t, "triaged.toml")

	res := runCLI(t, s, dir, "post", "2")
	if res.code != 0 {
		t.Fatalf("post failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(res.stdout))
	s.RequireAllPlayed(t)

	if _, err := os.Stat(filepath.Join(dir, ".second-look", "pr-2.toml")); !os.IsNotExist(err) {
		t.Fatal("the prepared review outlived a successful post, so posting again would post it twice")
	}
}

// TestPostDryRun pins the payload byte for byte. What stays local is the point:
// the notes, the skip reason, and the comment held back as a draft are all in
// the fixture and none of them may appear here.
func TestPostDryRun(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "guard-only", guardOnly))
	dir := workspace(t, "triaged.toml")

	res := runCLI(t, s, dir, "post", "2", "--dry-run")
	if res.code != 0 {
		t.Fatalf("dry run failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(res.stdout))
	s.RequireAllPlayed(t)

	for _, local := range []string{"\"note\"", "skip_reason", "strings.Split does this"} {
		if strings.Contains(res.stdout, local) {
			t.Errorf("the payload carries %q, which never leaves this laptop", local)
		}
	}
}

// TestPostRefusesDrafts is the staged review as an agent leaves it, with one
// comment still undecided. Nothing may post until a person rules on it.
// A finding worth saying now rather than at the end goes out on its own. The
// rest of the review stays staged, and the comment leaves the file because
// GitHub owns it from that moment: a copy left behind would go out twice.
func TestPostOneCommentOnItsOwn(t *testing.T) {
	t.Parallel()

	dir := workspace(t, "triaged.toml")
	s := ghcassette.Replay(t, derive(t, "post-one", func(c *ghcassette.Cassette) {
		// The review POST is replaced by the single-comment endpoint, which is
		// the one call this makes after the two reads.
		c.Interactions = c.Interactions[:reads+1]
		c.Interactions[reads].Args = []string{
			"api", "--method", "POST", "/repos/KyleKing/second-look/pulls/2/comments", "--input", "-",
		}
	}))

	res := runCLI(t, s, dir, "post", "2", "--only", "zero-is-not-empty")
	if res.code != 0 {
		t.Fatalf("post --only failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, "/pulls/2/comments") {
		t.Errorf("the standalone endpoint was not used:\n%s", res.stdout)
	}

	review, err := artifact.Load(filepath.Join(dir, ".second-look", "pr-2.toml"))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	for i := range review.Comments {
		if review.Comments[i].ID == "zero-is-not-empty" {
			t.Error("the posted comment is still staged and would go out twice")
		}
	}

	if len(review.Comments) == 0 {
		t.Error("posting one comment took the rest of the review with it")
	}

	s.RequireAllPlayed(t)
}

// A comment that was skipped was declined and a draft has not been ruled on, so
// neither goes out on its own however directly it is named.
func TestPostOneRefusesWhatWasNotRuledOn(t *testing.T) {
	t.Parallel()

	dir := workspace(t, "triaged.toml")
	s := ghcassette.Replay(t, derive(t, "guard-only-skip", guardOnly))

	res := runCLI(t, s, dir, "post", "2", "--only", "hand-rolled-split")
	if res.code == 0 {
		t.Fatalf("a skipped comment posted on its own:\n%s", res.stdout)
	}

	if !strings.Contains(res.stderr, "not posted") {
		t.Errorf("the refusal does not say why:\n%s", res.stderr)
	}
}

func TestPostRefusesDrafts(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "guard-only", guardOnly))
	dir := workspace(t, "staged.toml")

	res := runCLI(t, s, dir, "post", "2")
	if res.code == 0 {
		t.Fatal("expected a draft comment to block the post")
	}

	if !strings.Contains(res.stderr, "still draft") {
		t.Errorf("want the drafts named, got %q", res.stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, ".second-look", "pr-2.toml")); err != nil {
		t.Error("a refused post removed the prepared review")
	}
}

// TestPostRefusesMovedHead replays the recorded pull request with a different
// head commit, since waiting for the real branch to move is not a test.
func TestPostRefusesMovedHead(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "head-moved", headMoved))
	dir := workspace(t, "triaged.toml")

	res := runCLI(t, s, dir, "post", "2")
	if res.code == 0 {
		t.Fatal("expected a moved head to block the post")
	}

	if !strings.Contains(res.stderr, "new commits") || !strings.Contains(res.stderr, "second-look get 2") {
		t.Errorf("want the head mismatch and the command that fixes it, got %q", res.stderr)
	}
}

// TestCommentAddRefusesAnUnanchoredLine is the bot-cites-line-993 case, caught
// while staging against the real recorded diff rather than by GitHub.
func TestCommentAddRefusesAnUnanchoredLine(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "no-calls", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))
	dir := workspace(t, "triaged.toml")
	seedDiff(t, dir)

	batch := `{"comments":[{"id":"off-the-end","path":"testdata/fixture/sample.go","line":993,` +
		`"side":"RIGHT","body":"nope","note":"","severity":"nit","status":"ready"}]}`

	res := runCLIStdin(t, s, dir, batch, "comment", "add", "2")
	if res.code == 0 {
		t.Fatal("expected a line the diff does not carry to be refused")
	}

	if !strings.Contains(res.stderr, "nothing was written") {
		t.Errorf("want the refusal to say nothing was written, got %q", res.stderr)
	}
}

// guardOnly keeps the two reads the anchor guard makes and drops the post, for
// a run that is expected to stop before it sends anything.
func guardOnly(c *ghcassette.Cassette) {
	c.Interactions = c.Interactions[:reads]
}

// headMoved is guardOnly with the pull request reporting a head the review was
// not prepared against.
func headMoved(c *ghcassette.Cassette) {
	guardOnly(c)

	c.Interactions[0].Stdout = strings.Replace(
		c.Interactions[0].Stdout, fixtureHeadSHA, strings.Repeat("f", len(fixtureHeadSHA)), 1,
	)
}
