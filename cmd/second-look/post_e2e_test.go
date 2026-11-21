package main_test

import (
	"bytes"
	"os"
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

	if _, err := os.Stat(artifact.Path(stored(t, dir), 2)); !os.IsNotExist(err) {
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

	if !strings.Contains(res.stdout, "posted comment") {
		t.Errorf("the standalone comment was not reported:\n%s", res.stdout)
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
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

// TestCommentAddHoldsEveryStagedCommentAsADraft is the whole point of staging
// through a file: what an agent writes is a proposal, and the author rules on
// each one. A skip is left alone, since that is a finding already declined.
func TestCommentAddHoldsEveryStagedCommentAsADraft(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "no-calls", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))
	dir := workspace(t, "triaged.toml")
	seedDiff(t, dir)

	batch := `{"comments":[{"id":"held","path":"testdata/fixture/sample.go","line":17,` +
		`"side":"RIGHT","body":"wrap this","note":"","severity":"nit","status":"ready","origin":"me"}]}`

	res := runCLIStdin(t, s, dir, batch, "comment", "add", "2")
	if res.code != 0 {
		t.Fatalf("staging failed: %s", res.stderr)
	}

	if !strings.Contains(res.stdout, "held as draft") {
		t.Errorf("staging did not say what it held: %q", res.stdout)
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatal(err)
	}

	for i := range review.Comments {
		if review.Comments[i].ID == "held" && review.Comments[i].Status != artifact.StatusDraft {
			t.Errorf("a staged comment is %q, want draft", review.Comments[i].Status)
		}
		if review.Comments[i].ID == "held" && review.Comments[i].Origin != "agent" {
			t.Errorf("a staged comment is attributed to %q, want agent", review.Comments[i].Origin)
		}
	}
}

func TestRestagingKeepsTheAuthorsSavedComment(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "no-calls", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))
	dir := workspace(t, "triaged.toml")
	seedDiff(t, dir)

	batch := `{"comments":[{"id":"held","path":"testdata/fixture/sample.go","line":17,` +
		`"side":"RIGHT","body":"wrap this","note":"","severity":"nit","status":"ready"}]}`

	if res := runCLIStdin(t, s, dir, batch, "comment", "add", "2"); res.code != 0 {
		t.Fatalf("staging failed: %s", res.stderr)
	}

	path := artifact.Path(stored(t, dir), 2)

	review, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	review.Find("held").Origin = artifact.OriginMe
	if err := artifact.Save(path, review); err != nil {
		t.Fatal(err)
	}

	if res := runCLIStdin(t, s, dir, batch, "comment", "add", "2"); res.code != 0 {
		t.Fatalf("restaging failed: %s", res.stderr)
	}

	review, err = artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := review.Find("held").Origin; got != artifact.OriginMe {
		t.Errorf("restaging turned the author's saved comment into %q, want me", got)
	}
}

// TestCommandsRefuse is every command that stops short of what it was asked,
// and each one leaves the staged review exactly as it was. A skipped comment
// was declined, a draft has not been ruled on, and a todo is work an agent still
// owes, so none goes out however directly it is named; a get stops before any
// gh call where there is no repository; a pull request whose head moved names the get that
// fixes it, from a recording with a different head since waiting for the real
// branch to move is not a test; a line the diff does not carry is the
// bot-cites-line-993 case, caught while staging rather than by GitHub; and a
// command that takes no arguments keeps a typo from reading as something to do.
func TestCommandsRefuse(t *testing.T) {
	t.Parallel()

	noCalls := func(c *ghcassette.Cassette) { c.Interactions = nil }
	offTheEnd := `{"comments":[{"id":"off-the-end","path":"testdata/fixture/sample.go","line":993,` +
		`"side":"RIGHT","body":"nope","note":"","severity":"nit","status":"ready"}]}`

	for _, tc := range []refusal{
		{
			name: "post --only a skipped comment", fixture: "triaged.toml",
			cassette: "guard-only-skip", edit: guardOnly,
			args: []string{"post", "2", "--only", "hand-rolled-split"}, says: []string{"not posted"},
		},
		{
			name: "post over a draft", fixture: "staged.toml", cassette: "guard-only", edit: guardOnly,
			args: []string{"post", "2"}, says: []string{"still draft"},
		},
		{
			name: "post onto a moved head", fixture: "triaged.toml", cassette: "head-moved", edit: headMoved,
			args: []string{"post", "2"}, says: []string{"new commits", "second-look get 2"},
		},
		{
			name: "comment add off the diff", fixture: "triaged.toml", cassette: "no-calls", edit: noCalls,
			stdin: offTheEnd, args: []string{"comment", "add", "2"}, says: []string{"nothing was written"},
		},
		{
			name: "post while work is outstanding", fixture: "triaged.toml", cassette: "todo-blocks", edit: guardOnly,
			prep: func(r *artifact.Review) { r.Comments[0].Status = artifact.StatusTodo },
			args: []string{"post", "2"}, says: []string{"still todo"},
		},
		{
			name: "get outside a repository", cassette: "no-calls", edit: noCalls,
			args: []string{"get", "2"}, says: []string{"not a git or jj repository"},
		},
		{
			name: "skill with an argument", cassette: "no-calls", edit: noCalls,
			args: []string{"skill", "install"}, says: []string{"usage: second-look skill"},
		},
		{
			name: "status with an argument", cassette: "status-args", edit: noCalls,
			args: []string{"status", "extra"}, says: []string{"usage: second-look status"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t)
		})
	}
}

// refusal is one command that must stop, run against a staged fixture where it
// names one, and what its refusal has to say.
type refusal struct {
	name, fixture, cassette, stdin string
	edit                           func(*ghcassette.Cassette)
	prep                           func(*artifact.Review)
	args, says                     []string
}

func (tc *refusal) check(t *testing.T) {
	t.Helper()

	dir, path := t.TempDir(), ""
	if tc.fixture != "" {
		dir = workspace(t, tc.fixture)
		seedDiff(t, dir)
		path = artifact.Path(stored(t, dir), 2)
	}

	if tc.prep != nil {
		r, err := artifact.Load(path)
		if err != nil {
			t.Fatal(err)
		}

		tc.prep(r)

		if err := artifact.Save(path, r); err != nil {
			t.Fatal(err)
		}
	}

	var before []byte

	if path != "" {
		var err error
		if before, err = os.ReadFile(path); err != nil { // #nosec G304 -- the fixture this test staged
			t.Fatal(err)
		}
	}

	s := ghcassette.Replay(t, derive(t, tc.cassette, tc.edit))

	res := runCLIStdin(t, s, dir, tc.stdin, tc.args...)
	if res.code == 0 {
		t.Fatalf("%v ran to success:\n%s", tc.args, res.stdout)
	}

	for _, want := range tc.says {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, res.stderr)
		}
	}

	if path == "" {
		return
	}

	// #nosec G304 -- the fixture this test staged
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
		t.Errorf("a refused %v changed the staged review (%v)", tc.args, err)
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
