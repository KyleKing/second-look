package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/ghcassette"
)

// TestGetPreparesTheReview is get standing on the head already, which is what
// `gh pr checkout` then `second-look get` looks like. Nothing moves; the
// artifact and the cached diff are written and the diff is the recorded bytes.
func TestGetPreparesTheReview(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))

	res := runCLI(t, s, dir, "get", "2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(anonymize(res.stdout, sha)))
	s.RequireAllPlayed(t)

	review, err := artifact.Load(filepath.Join(dir, ".second-look", "pr-2.toml"))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	if review.Owner != "KyleKing" || review.Repo != "second-look" || review.Number != 2 {
		t.Errorf("the review names %s/%s#%d, which is not the remote", review.Owner, review.Repo, review.Number)
	}

	// #nosec G304 -- a path under the test's own temporary directory
	cached, err := os.ReadFile(filepath.Join(dir, ".second-look", "diff", sha+".patch"))
	if err != nil {
		t.Fatalf("the cached diff: %v", err)
	}

	if !strings.Contains(string(cached), "+++ b/testdata/fixture/sample.go") {
		t.Error("the cached diff is not the one the recording carried")
	}
}

// TestGetCarriesStagedCommentsOntoANewHead is the re-run after the branch moves.
// The comments stay, and get says so, because the anchor guard re-checks each
// one on post rather than here.
func TestGetCarriesStagedCommentsOntoANewHead(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))

	// The fixture is staged against the recorded head; the pull request now
	// reports the scratch repository's.
	seedReview(t, dir, fixtureHeadSHA)

	res := runCLI(t, s, dir, "get", "2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(anonymize(res.stdout, sha)))

	review, err := artifact.Load(filepath.Join(dir, ".second-look", "pr-2.toml"))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	if review.HeadSHA != sha {
		t.Errorf("the review is still staged against %s", review.HeadSHA)
	}

	if len(review.Comments) != 5 {
		t.Errorf("%d comment(s) survived the move, want 5", len(review.Comments))
	}
}

// TestGetRefusesToMoveADirtyTree is the guard that exists because moving the
// working copy is the only thing get does that can lose someone's work.
func TestGetRefusesToMoveADirtyTree(t *testing.T) {
	t.Parallel()

	dir, _ := scratchRepo(t, "some-other-branch")
	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "elsewhere", func(c *ghcassette.Cassette) {
		c.Interactions = c.Interactions[:1]
	}))

	if err := os.WriteFile(filepath.Join(dir, "unsaved.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("dirtying the tree: %v", err)
	}

	res := runCLI(t, s, dir, "get", "2")
	if res.code == 0 {
		t.Fatal("expected a dirty tree to block the checkout")
	}

	if !strings.Contains(res.stderr, "uncommitted changes") {
		t.Errorf("want the dirty tree named, got %q", res.stderr)
	}
}

// TestGetRefusesSomewhereThatIsNotARepo stops before any gh call, so the
// cassette is empty and a single request would fail the test.
func TestGetRefusesSomewhereThatIsNotARepo(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "no-calls", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))

	res := runCLI(t, s, t.TempDir(), "get", "2")
	if res.code == 0 {
		t.Fatal("expected a directory that is not a repository to be refused")
	}

	if !strings.Contains(res.stderr, "not a git or jj repository") {
		t.Errorf("want the missing repository named, got %q", res.stderr)
	}
}

// anonymize replaces the scratch repository's commit, which is a different one
// on every run, so the output can be pinned.
func anonymize(out, sha string) string {
	return strings.ReplaceAll(out, sha[:7], "<head>")
}
