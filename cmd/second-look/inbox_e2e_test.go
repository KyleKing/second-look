package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"
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
// GHCASSETTE_RECORD=1 would overwrite it with real data. Do not.
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

	// The queue is ordered for triage rather than by the order the searches
	// answered, so the row is named rather than assumed to be the first.
	sc.press("/aragonite#100")
	sc.press("\r")
	sc.press("\r")
	sc.await("kyleking/aragonite #100")

	sc.press("q")
	sc.wait()
}

// inboxThenReview is the three searches, then what opening the first row costs,
// addressed to the pull request that row names.
func inboxThenReview(t *testing.T) string {
	t.Helper()

	c := load(t, "inbox")
	recorded := load(t, "post-review")

	opening := make([]ghcassette.Interaction, 0, reads+1)
	opening = append(opening, recorded.Interactions[:reads]...)
	opening = append(opening, threadInteraction(t)...)

	for i := range opening {
		c.Interactions = append(c.Interactions, addressed(opening[i], "kyleking/aragonite", 100))
	}

	path := filepath.Join(t.TempDir(), "inbox-review.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

// TestInboxScreenApprovesOnTheSecondPress is the one verb on a row that sends
// something GitHub keeps. One press arms it and says so, and the second sends
// the approval the cassette expects.
func TestInboxScreenApprovesOnTheSecondPress(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, withApprove(t))
	home := t.TempDir()

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "inbox")
	sc.await("kyleking/aragonite#100")

	sc.press("/aragonite#100")
	sc.press("\r")

	sc.press("A")
	sc.await("A again to approve")

	// The footer clips to the frame, so what it says is checked short and the
	// cassette is what proves the request went out.
	sc.press("A")
	sc.await("approved")

	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}

// withApprove is the three searches plus the approval the second A sends. The
// answer is empty because gh prints nothing on success and the exit code is
// what says it worked.
func withApprove(t *testing.T) string {
	t.Helper()

	c := load(t, "inbox")
	c.Interactions = append(c.Interactions, ghcassette.Interaction{
		Args: []string{"pr", "review", "100", "--repo", "kyleking/aragonite", "--approve"},
	})

	path := filepath.Join(t.TempDir(), "inbox-approve.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

// TestInboxScreenCommentsThroughTheEditor is the one verb that leaves the screen
// and comes back: m closes it, $EDITOR writes the body, gh posts it on the pull
// request itself, and the queue is drawn again.
func TestInboxScreenCommentsThroughTheEditor(t *testing.T) {
	t.Parallel()

	const body = "Rebased this onto main, CI is green now."

	s := ghcassette.Replay(t, withComment(t, body))
	home := t.TempDir()

	editor := filepath.Join(t.TempDir(), "editor")
	script := "#!/bin/sh\nprintf '" + body + "\\n' > \"$1\"\n"

	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the editor: %v", err)
	}

	sc := openReview(t, s, t.TempDir(),
		"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "EDITOR="+editor, "inbox")
	sc.await("kyleking/aragonite#100")

	// Everything after this point is a second draw of text the first screen
	// already wrote, so the wait has to start where the first one left off.
	sc.press("/aragonite#100")
	sc.press("\r")

	from := sc.mark()

	sc.press("m")
	sc.awaitFrom(from, "commented on kyleking/aragonite#100")

	// The queue comes back, which is what makes a comment a detour rather than
	// the end of the session.
	sc.awaitFrom(from, "pending your review")

	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}

// withComment is the searches, the comment m sends, then the searches again for
// the queue it returns to.
func withComment(t *testing.T, body string) string {
	t.Helper()

	c := load(t, "inbox")
	searches := c.Interactions

	c.Interactions = append(append([]ghcassette.Interaction{}, searches...), ghcassette.Interaction{
		Args: []string{"pr", "comment", "100", "--repo", "kyleking/aragonite", "--body", body},
	})
	c.Interactions = append(c.Interactions, searches...)

	path := filepath.Join(t.TempDir(), "inbox-comment.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

// A config that will not parse leaves a working queue, and its complaint goes to
// stderr: --json puts a document on stdout and a warning in front of it is a
// document nothing can parse.
func TestInboxWithABrokenConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	dir := filepath.Join(home, ".config", "second-look")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[[section]]\nname = \"mine\"\nfilters = \"is:open\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := ghcassette.Replay(t, cassettePath(t, "inbox"))

	res := runCLIEnv(t, s, t.TempDir(), homeEnv(home), "inbox", "--json")
	if res.code != 0 {
		t.Fatalf("inbox --json failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.HasPrefix(strings.TrimSpace(res.stdout), "[") {
		t.Errorf("the warning landed in the JSON:\n%s", res.stdout)
	}

	if !strings.Contains(res.stderr, "filters") {
		t.Errorf("the broken key was never named:\n%s", res.stderr)
	}

	// The built-in buckets are what a broken config falls back to.
	if !strings.Contains(res.stdout, "pending your review") {
		t.Errorf("the queue is empty rather than built-in:\n%s", res.stdout)
	}

	s.RequireAllPlayed(t)
}
