package main_test

import (
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
