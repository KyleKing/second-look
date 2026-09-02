package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"
)

// `reviews` reads the directory and nothing else, so its cassette is empty and
// a single gh call would fail the test.
func TestReviews(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)
	broken(t, dir, 9)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "reviews", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	res := runCLI(t, s, dir, "reviews")
	if res.code != 0 {
		t.Fatalf("reviews failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{
		"KyleKing/second-look#2",
		// A file that no longer parses is the row most worth knowing about, so it
		// is listed with its reason rather than skipped.
		"#9",
		"unreadable",
		"pr-9.toml",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("%q is missing:\n%s", want, res.stdout)
		}
	}
}

// An empty checkout is the common case on a laptop with nothing staged, and it
// answers rather than failing.
func TestReviewsWithNothingStaged(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "reviews-empty", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	res := runCLI(t, s, t.TempDir(), "reviews")
	if res.code != 0 {
		t.Fatalf("reviews failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, "nothing") {
		t.Errorf("an empty checkout said %q", res.stdout)
	}
}

func TestReviewsJSON(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "reviews-json", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	res := runCLI(t, s, dir, "reviews", "--json")
	if res.code != 0 {
		t.Fatalf("reviews --json failed: %s%s", res.stdout, res.stderr)
	}

	var rows []struct {
		Path    string `json:"path"`
		Number  int    `json:"number"`
		Ready   int    `json:"ready"`
		HeadSHA string `json:"head_sha"`
	}

	if err := json.Unmarshal([]byte(res.stdout), &rows); err != nil {
		t.Fatalf("reading the rows: %v\n%s", err, res.stdout)
	}

	if len(rows) != 1 || rows[0].Number != 2 || rows[0].HeadSHA != sha {
		t.Errorf("printed %+v, want one row for #2 at %s", rows, sha)
	}
}

// broken writes an artifact that no longer parses, which is what a hand-edit
// gone wrong leaves behind.
func broken(t *testing.T, dir string, number int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, ".second-look"), 0o750); err != nil {
		t.Fatalf("creating the artifact directory: %v", err)
	}

	path := filepath.Join(dir, ".second-look", fmt.Sprintf("pr-%d.toml", number))
	if err := os.WriteFile(path, []byte("version = 1\nowner = \n"), 0o600); err != nil {
		t.Fatalf("writing the broken review: %v", err)
	}
}

// The staged-review screen on a terminal: the rows are drawn, a file that no
// longer parses says why rather than opening into an empty review, and the tab
// it opened on is one of three. The other two are not loaded, which the empty
// cassette is what proves: a tab nobody switched to makes no request.
func TestReviewsScreen(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)
	broken(t, dir, 9)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "reviews-screen", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	sc := openReview(t, s, dir, "reviews")
	sc.await("second-look staged reviews")
	sc.await("[3] staged")
	sc.await("staged under .second-look")
	sc.await("KyleKing/second-look#2")

	// The unreadable row is first, being the newest, and choosing it reports the
	// reason rather than opening a review that could not be read.
	sc.press("\r")
	sc.await("cannot be read")

	// d asks first, because what it deletes never posted and is the only copy.
	at := sc.mark()

	sc.press("d")
	sc.awaitFrom(at, "d again to discard")
	sc.press("d")
	sc.awaitFrom(at, "#9; 1 staged · 1 blocked")

	if _, err := os.Stat(filepath.Join(dir, ".second-look", "pr-9.toml")); !os.IsNotExist(err) {
		t.Errorf("the discarded review is still on disk: %v", err)
	}

	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the list exited %d:\n%s", code, sc.text())
	}
}
