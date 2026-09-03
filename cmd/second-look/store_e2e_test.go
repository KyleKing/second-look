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
// directories. Reading it from two places used to stage it twice.
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
