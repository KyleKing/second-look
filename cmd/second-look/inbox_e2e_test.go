package main_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/ghcassette"
)

// The inbox is three gh searches and nothing local, so this is the one test in
// this package that runs outside a checkout.
//
// Its cassette is the only one here that is written rather than recorded. A
// real recording of these searches carries private repository names, the
// usernames of everyone whose pull requests are waiting, and their titles, and
// none of that belongs in a public repository. The arguments are the ones a
// real run made and the answers are gh's own shape with invented content, which
// is what this test needs and nothing more.
//
// SECOND_LOOK_RECORD=1 would overwrite it with real data. Do not.
func TestInbox(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := ghcassette.Replay(t, cassettePath(t, "inbox"))

	res := runCLI(t, s, dir, "inbox")
	if res.code != 0 {
		t.Fatalf("inbox failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{"pending your review", "reviewed, still open", "reviewed, merged"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("the %q bucket is missing:\n%s", want, res.stdout)
		}
	}

	s.RequireAllPlayed(t)
}

// --json carries what the human view trims: the whole of a failure, and the
// fields a script sorts on.
func TestInboxJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := ghcassette.Replay(t, cassettePath(t, "inbox"))

	res := runCLI(t, s, dir, "inbox", "--json")
	if res.code != 0 {
		t.Fatalf("inbox --json failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.HasPrefix(strings.TrimSpace(res.stdout), "[") {
		t.Errorf("--json did not print JSON:\n%s", res.stdout)
	}

	for _, want := range []string{`"bucket"`, `"repository"`, `"updated"`} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("the %s field is missing:\n%s", want, res.stdout)
		}
	}
}

// TestInboxScreenOpensAReviewWithNoClone is the queue doing what a dashboard is
// for: enter on a row opens the review, and the row it lands on is a repository
// this laptop has no clone of, so opening it costs the two API reads and
// nothing else.
func TestInboxScreenOpensAReviewWithNoClone(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	s := ghcassette.Replay(t, inboxThenReview(t))

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "inbox")
	sc.await("pending your review")
	sc.await("kyleking/aragonite#100")

	sc.press("\r")
	sc.await("kyleking/aragonite #100")

	sc.press("q")
	sc.wait()
}

// inboxThenReview is the three searches, then the two reads that opening the
// first row costs, addressed to the pull request that row names.
func inboxThenReview(t *testing.T) string {
	t.Helper()

	c := load(t, "inbox")
	recorded := load(t, "post-review")

	for i := range recorded.Interactions[:reads] {
		in := recorded.Interactions[i]

		for j, arg := range in.Args {
			if arg == "2" {
				in.Args[j] = "100"
			}

			if arg == fixtureRepo {
				in.Args[j] = "kyleking/aragonite"
			}
		}

		in.Stdout = strings.ReplaceAll(in.Stdout, `"number":2`, `"number":100`)
		c.Interactions = append(c.Interactions, in)
	}

	path := filepath.Join(t.TempDir(), "inbox-review.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}
