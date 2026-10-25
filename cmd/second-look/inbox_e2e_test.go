package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	main "github.com/kyleking/second-look/cmd/second-look"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/labels"
)

// The inbox is four gh searches and the one batched read that sorts their rows
// into lanes, and nothing local, so this is the one test in this package that
// runs outside a checkout.
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
	s := ghcassette.Replay(t, inboxCassettePath(t))

	res := runCLI(t, s, dir, "inbox")
	if res.code != 0 {
		t.Fatalf("inbox failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{
		"ready to merge", "back to you", "new for you", "waiting on others", "merged",
		"only you", "+ 2", "approved, checks passing", "2 new comments", "pushed since your review",
	} {
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
	s := ghcassette.Replay(t, inboxCassettePath(t))

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

// Whatever reviews a queue in turn reads the piped shape, so it has to arrive in
// the order the screen draws and carry the rating the screen shows. Without
// both, a driver re-fetches every diff to work out which row is cheap and then
// works through them by recency anyway.
func TestInboxJSONCarriesTheRatingAndTheTriageOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := ghcassette.Replay(t, inboxCassettePath(t))

	// The row of the new lane that was staged and rated here already, which is
	// what puts it ahead of the one carrying a priority label: finishing a
	// review costs less than beginning one.
	const (
		sha  = "aa11bb22cc33dd44ee55ff6677889900aabbccdd"
		cost = 37
	)

	root := storeFor(t, testHome(t, dir), "kyleking", "gh-sweep")
	staged := fmt.Sprintf("version = 1\nhost = 'github.com'\nowner = 'kyleking'\n"+
		"repo = 'gh-sweep'\nnumber = 102\nhead_sha = '%s'\n"+
		"[[comment]]\nid = 'c1'\npath = 'a.go'\nline = 3\nside = 'RIGHT'\n"+
		"status = 'ready'\nseverity = 'nit'\nbody = 'name it'\n", sha)

	write(t, artifact.Path(root, 102), []byte(staged))

	if err := artifact.SaveScore(root, sha, artifact.Cost{Total: cost, Added: 12, Removed: 3}); err != nil {
		t.Fatalf("seeding the rating: %v", err)
	}

	res := runCLI(t, s, dir, "inbox", "--json")
	if res.code != 0 {
		t.Fatalf("inbox --json failed: %s%s", res.stdout, res.stderr)
	}

	var buckets []struct {
		Bucket string `json:"bucket"`
		Items  []struct {
			Repository string `json:"repository"`
			Number     int    `json:"number"`
			Reviewed   bool   `json:"reviewed"`
			Who        string `json:"who"`
			Cost       int    `json:"cost"`
			Rated      bool   `json:"rated"`
			Added      int    `json:"added"`
			Ready      int    `json:"ready"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(res.stdout), &buckets); err != nil {
		t.Fatalf("reading the queue: %v\n%s", err, res.stdout)
	}

	var lane int

	for i := range buckets {
		if buckets[i].Bucket == "new for you" {
			lane = i
		}
	}

	if len(buckets[lane].Items) == 0 {
		t.Fatalf("the lane is empty, so nothing here is exercised:\n%s", res.stdout)
	}

	first := buckets[lane].Items[0]
	if first.Repository != "kyleking/gh-sweep" || first.Number != 102 {
		t.Errorf("the bucket leads with %s#%d, want the review already staged here",
			first.Repository, first.Number)
	}

	if first.Who != "only you" {
		t.Errorf("the row says %q is waiting on it, want the one human asked", first.Who)
	}

	if !first.Reviewed || !first.Rated || first.Cost != cost || first.Added != 12 {
		t.Errorf("the row carries %+v, want the rating read off disk", first)
	}

	// What the review already holds is what says how far the last sitting got,
	// and the screen draws it beside the row.
	if first.Ready != 1 {
		t.Errorf("the row carries %d ready comment(s), want the one staged here", first.Ready)
	}
}

// TestInboxScreenOpensAReviewWithNoClone is the queue doing what a dashboard is
// for: enter on a row opens the review, and the row it lands on is a repository
// this laptop has no clone of, so opening it costs the two API reads and
// nothing else.
func TestInboxScreenOpensAReviewWithNoClone(t *testing.T) {
	t.Parallel()

	home := quietHome(t)
	s := ghcassette.Replay(t, inboxThenReview(t))

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "inbox")
	sc.await("new for you")
	sc.await("aragonite#100")

	// The queue is ordered for triage rather than by the order the searches
	// answered, so the row is named rather than assumed to be the first.
	sc.press("/aragonite#100")
	sc.press("\r")
	sc.press("\r")
	sc.await("kyleking/aragonite #100")

	// The palette is Ink whatever the terminal is, so nothing asks for the
	// background, and a query against the program's own reader would deadlock.
	if n := strings.Count(sc.raw(), "\x1b]11;?"); n != 0 {
		t.Errorf("the terminal was asked its background color %d times, want never", n)
	}

	// Leaving the review comes back to the queue rather than ending the
	// session, which is what makes twenty-five reviews one sitting. It is the
	// same queue: the filter it was narrowed to is still on, and the cassette
	// holds one round of searches, so nothing was looked up again.
	at := sc.mark()

	sc.press("q")
	sc.awaitFrom(at, "showing 1 of")

	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}

// inboxCassette is the written searches plus the batched read of reviews. Its
// arguments come from inbox.FactsArgs and the ids the searches carry, so the
// call it answers is the call the queue makes, and the reply is the one fixture
// of that shape.
func inboxCassette(t *testing.T) *ghcassette.Cassette {
	t.Helper()

	c := load(t, "inbox")

	var ids []string

	for _, in := range c.Interactions {
		if slices.Contains(in.Args, "--merged") {
			continue
		}

		var rows []struct {
			ID string `json:"id"`
		}

		if err := json.Unmarshal([]byte(in.Stdout), &rows); err != nil {
			t.Fatalf("reading a search answer: %v", err)
		}

		for _, r := range rows {
			ids = append(ids, r.ID)
		}
	}

	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "inbox", "testdata", "facts.json"))
	if err != nil {
		t.Fatalf("reading the facts fixture: %v", err)
	}

	c.Interactions = append(c.Interactions, ghcassette.Interaction{Args: inbox.FactsArgs(ids), Stdout: string(body)})

	return c
}

func inboxCassettePath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "inbox.golden")
	if err := ghcassette.Save(path, inboxCassette(t)); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

// onScreen is the inbox recording plus the one call the screen makes and the
// listing does not: before rating anything the queue reads what is left of the
// hourly allowance, and that read is free.
func onScreen(t *testing.T) *ghcassette.Cassette {
	t.Helper()

	c := inboxCassette(t)
	c.Interactions = append(c.Interactions, ghcassette.Interaction{
		Args: []string{"api", "rate_limit"},
		Stdout: `{"resources":{"core":{"limit":5000,"remaining":4900,"reset":1788400000},` +
			`"graphql":{"limit":5000,"remaining":5000,"reset":1788400000},` +
			`"search":{"limit":30,"remaining":30,"reset":1788400000}}}`,
	})

	return c
}

// inboxThenReview is the three searches, then what opening the first row costs,
// addressed to the pull request that row names.
func inboxThenReview(t *testing.T) string {
	t.Helper()

	c := onScreen(t)
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
	home := quietHome(t)

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "inbox")
	sc.await("aragonite#100")

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

	c := onScreen(t)
	c.Interactions = append(c.Interactions, ghcassette.Interaction{
		Args: []string{"pr", "review", "100", "--repo", "kyleking/aragonite", "--approve"},
	})

	path := filepath.Join(t.TempDir(), "inbox-approve.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	return path
}

// M merges a pull request that is approved with passing checks, on the second
// press, with the squash the review screen's M makes. On any other row it does
// nothing and says why, which the cassette proves by holding no merge for it.
func TestInboxScreenMergesOnTheSecondPress(t *testing.T) {
	t.Parallel()

	c := onScreen(t)
	c.Interactions = append(c.Interactions, ghcassette.Interaction{
		Args: []string{"pr", "merge", "130", "--squash", "--delete-branch", "--repo", "kyleking/aragonite"},
	})

	path := filepath.Join(t.TempDir(), "inbox-merge.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	s := ghcassette.Replay(t, path)
	home := quietHome(t)

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "inbox")
	sc.await("approved,")

	sc.press("/aragonite#130")
	sc.press("\r")
	sc.press("M")
	sc.await("M again to squash-merge")

	sc.press("M")
	sc.await("merged kyleking/aragonite#130")

	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}

// TestInboxScreenCommentsThroughTheEditor is the one verb that leaves the screen
// and comes back: m closes it, $EDITOR writes the body, gh posts it on the pull
// request itself, and the queue is drawn again.
func TestInboxScreenCommentsThroughTheEditor(t *testing.T) {
	t.Parallel()

	const body = "Rebased this onto main, CI is green now."

	cassette := withComment(t, body)
	s := ghcassette.Replay(t, cassette)
	home := quietHome(t)

	editor := filepath.Join(t.TempDir(), "editor")
	script := "#!/bin/sh\nprintf '" + body + "\\n' > \"$1\"\n"

	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil { //nolint:gosec // it has to run
		t.Fatalf("writing the editor: %v", err)
	}

	sc := openReview(t, s, t.TempDir(),
		"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "EDITOR="+editor, "inbox")
	sc.await("aragonite#100")

	// Everything after this point is a second draw of text the first screen
	// already wrote, so the wait has to start where the first one left off.
	sc.press("/aragonite#100")
	sc.press("\r")

	from := sc.mark()

	sc.press("m")
	back := sc.awaitFrom(from, "commented on kyleking/aragonite#100")

	// The redraw rewrites only changed digits, so the header count never appears
	// whole. A row is written whole, so an excluded row's absence proves the filter.
	sc.awaitFrom(back, "kyleking/aragonite#100  octocat")

	if after := sc.text()[back:]; strings.Contains(after, "octocat/hello-world#112") {
		t.Errorf("the queue came back unnarrowed, listing a row the filter excludes:\n%s", after)
	}

	recorded, err := ghcassette.Load(cassette)
	if err != nil {
		t.Fatalf("loading the derived cassette: %v", err)
	}

	sc.awaitPlayed(s, len(recorded.Interactions))

	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}

// withComment is the searches, the comment m sends, then the searches again for
// the queue it returns to.
func withComment(t *testing.T, body string) string {
	t.Helper()

	c := onScreen(t)
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

	home := quietHome(t)

	dir := filepath.Join(home, ".config", "second-look")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[[section]]\nname = \"mine\"\nfilters = \"is:open\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := ghcassette.Replay(t, inboxCassettePath(t))

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
	if !strings.Contains(res.stdout, "new for you") {
		t.Errorf("the queue is empty rather than built-in:\n%s", res.stdout)
	}

	s.RequireAllPlayed(t)
}

// The mark says a row was started before the counts say how far, since the
// queue already ranks started work first and never said so.
func TestTheQueueSaysWhatAStagedReviewHolds(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name                  string
		ready, draft, replies int
		want                  string
	}{
		{"nothing yet", 0, 0, 0, "● staged  "},
		{"one of each", 1, 1, 1, "● 1 ready 1 draft 1 reply  "},
		{"several", 4, 0, 2, "● 4 ready 2 replies  "},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := main.StagedMark(c.ready, c.draft, c.replies); got != c.want {
				t.Errorf("StagedMark() is %q, want %q", got, c.want)
			}
		})
	}
}

// l on a row picks labels from the repository's, one `gh label list` for the
// repository, and enter sets or clears the one under the cursor through the
// issues endpoint. The row says so before the forge answers, and the cassette
// holds the three calls that prove which ones went out.
func TestInboxScreenLabelsAPullRequest(t *testing.T) {
	t.Parallel()

	at := labels.Target{Repo: "kyleking/aragonite", Number: 100}

	c := onScreen(t)
	c.Interactions = append(c.Interactions,
		ghcassette.Interaction{
			Args: labels.ListArgs(at.Repo),
			Stdout: `[{"color":"5319e7","description":"hand the fixes to the bot","name":"watch-doggo-fix"},` +
				`{"color":"d4c5f9","description":"","name":"needs-review"}]`,
		},
		ghcassette.Interaction{Args: labels.SetArgs(at, "watch-doggo-fix", true), Stdout: "[]"},
		ghcassette.Interaction{Args: labels.SetArgs(at, "needs-review", false), Stdout: "[]"},
	)

	path := filepath.Join(t.TempDir(), "inbox-labels.golden")
	if err := ghcassette.Save(path, c); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	s := ghcassette.Replay(t, path)
	home := quietHome(t)

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "inbox")
	sc.await("aragonite#100")
	sc.press("/aragonite#100")
	sc.press("\r")

	sc.press("l")
	sc.await("hand the fixes to the bot")
	sc.press("doggo")
	sc.press("\r")
	sc.await("[watch-doggo-fix]")

	sc.press("\x7f\x7f\x7f\x7f\x7f")
	sc.press("needs")
	sc.press("\r")
	sc.await("removed needs-review")
	sc.press("\x1b")
	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}
