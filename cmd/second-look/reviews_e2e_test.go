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
	"github.com/kyleking/second-look/internal/agents"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/prstate"
	"github.com/kyleking/second-look/internal/tui"
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
	sc.await("left in a working copy")
	sc.await("KyleKing/second-look#2")

	// A file that could not be read names no repository, so nothing could move it
	// into the store and it lists as a leftover. Choosing it reports the reason
	// rather than opening a review that could not be read.
	sc.press("j")
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

// gc is the destructive twin of the list: a staged review whose pull request
// is finished goes, caches and all. --dry-run says what would go and changes
// nothing, and a review the forge cannot answer for is kept rather than
// guessed away.
func TestGC(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)
	seedDiffAt(t, dir, sha)
	broken(t, dir, 9)

	merged := func() ghcassette.Interaction {
		return ghcassette.Interaction{
			Args: []string{
				"api", "graphql",
				"-F", "owner=KyleKing", "-F", "repo=second-look", "-F", "pr=2",
				"-f", "query=" + prstate.Query,
			},
			Stdout: `{"data":{"repository":{"pullRequest":{"state":"MERGED",` +
				`"reviewDecision":"APPROVED","viewerLatestReview":{"state":"APPROVED"}}}}}`,
		}
	}

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "gc", func(c *ghcassette.Cassette) {
		c.Interactions = []ghcassette.Interaction{merged(), merged()}
	}))

	review := artifact.Path(stored(t, dir), 2)
	patch := artifact.DiffPath(stored(t, dir), sha)

	res := runCLI(t, s, dir, "gc", "--dry-run")
	if res.code != 0 {
		t.Fatalf("gc --dry-run failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{
		"would drop KyleKing/second-look#2 (merged)",
		"kept #9 (its state could not be read)",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("%q is missing:\n%s", want, res.stdout)
		}
	}

	if _, err := os.Stat(review); err != nil {
		t.Errorf("a dry run removed the review: %v", err)
	}

	res = runCLI(t, s, dir, "gc")
	if res.code != 0 {
		t.Fatalf("gc failed: %s%s", res.stdout, res.stderr)
	}

	for _, want := range []string{
		"dropped KyleKing/second-look#2 (merged)",
		"dropped 1 staged review",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("%q is missing:\n%s", want, res.stdout)
		}
	}

	for _, path := range []string{review, patch} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still on disk: %v", path, err)
		}
	}
}

// The indicator marks the row the directory stands on and the rows it cannot
// reach, and nothing else: a tree of one repository would otherwise repeat
// itself on every row.
func TestAStagedRowSaysWhetherThisDirectoryHoldsItsCode(t *testing.T) {
	t.Parallel()

	review := prepared.Review{
		Repository: "coverbasedev/irm", Number: 14798,
		HeadSHA: "60f9fb9", Ready: 1,
	}

	const held = "1 ready"

	for _, tc := range []struct {
		name   string
		repo   string
		head   string
		want   string
		remote []prstate.State
		acts   bool
	}{
		{
			// The one thing the local file cannot say is whether the work is
			// still wanted.
			name: "merged while it sat here", repo: "coverbasedev/irm", head: "60f9fb9",
			remote: []prstate.State{{State: "MERGED"}},
			want:   held + " · here · merged", acts: true,
		},
		{
			name: "standing on it", repo: "coverbasedev/irm", head: "60f9fb9",
			want: held + " · here", acts: true,
		},
		{
			name: "the same repository elsewhere", repo: "coverbasedev/irm", head: "aaaaaaa",
			want: held, acts: true,
		},
		// A row this directory cannot reach says nothing at all: a queue of
		// several repositories repeating it on every row is noise, and a C
		// pressed anyway names the refusal in the footer.
		{name: "another repository", repo: "deanmalmgren/textract", head: "aaaaaaa", want: held},
		{name: "no checkout at all", want: held},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, acts := main.StagedRow(review, tc.repo, tc.head, tc.remote...)
			if got != tc.want {
				t.Errorf("the row says %q, want %q", got, tc.want)
			}

			if acts != tc.acts {
				t.Errorf("C acting on it is %v, want %v", acts, tc.acts)
			}
		})
	}
}

// The row's last word is what the session recorded on it is doing: a blocked
// agent is the loudest thing on the list, a finished one is work to read, and
// one the listing no longer names ended, which is nothing to say.
func TestAStagedRowSaysWhatItsAgentIsDoing(t *testing.T) {
	t.Parallel()

	live := map[string]agents.Live{
		"sess-blocked": {Session: "sess-blocked", State: agents.Blocked},
		"sess-done":    {Session: "sess-done", State: agents.Done},
		"sess-busy":    {Session: "sess-busy", State: "busy"},
	}

	for _, tc := range []struct {
		name    string
		session string
		want    string
		tone    tui.Tone
	}{
		{name: "asking a question", session: "sess-blocked", want: "agent blocked", tone: tui.ToneWarn},
		{name: "work waiting to be read", session: "sess-done", want: "agent done", tone: tui.ToneGood},
		{name: "still running", session: "sess-busy", want: "agent busy", tone: tui.ToneMuted},
		{name: "ended", session: "sess-gone", want: "", tone: tui.ToneOrdinary},
		{name: "none recorded", want: "", tone: tui.ToneOrdinary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			review := prepared.Review{
				Repository: "acme/api", Number: 5,
				Agent: artifact.Agent{Session: tc.session},
			}

			got, tone := main.StagedAgent(review, live)
			if got != tc.want || tone != tc.tone {
				t.Errorf("the row says %q/%v, want %q/%v", got, tone, tc.want, tc.tone)
			}
		})
	}
}

// The staged tab is a queue, not a history: what needs a hand sorts ahead of
// what is already finished, and a pull request the forge has closed or merged
// sorts after everything, however much work the file holds.
func TestTheStagedTabReadsAsAQueue(t *testing.T) {
	t.Parallel()

	rows := []prepared.Review{
		{Repository: "acme/api", Number: 5},
		{Repository: "acme/api", Number: 1, Ready: 1},
		{Repository: "acme/api", Number: 3, Draft: 1},
		{Repository: "acme/api", Number: 4, Broken: "nope"},
		{Repository: "acme/api", Number: 2, Ready: 2},
	}
	remote := map[string]prstate.State{
		"acme/api#2": {State: "MERGED"},
		"acme/api#3": {State: "CLOSED"},
	}

	want := []string{"acme/api#1", "acme/api#5", "acme/api#4", "acme/api#3", "acme/api#2"}
	if got := main.StagedOrder(rows, remote); !slices.Equal(got, want) {
		t.Errorf("the queue reads %v, want %v", got, want)
	}
}

// Posting one review moves to the next, and the next is the same repository's
// where it has another staged: a sitting is one repository at a time and the
// clone is already standing on it.
func TestTheNextReviewAfterAPostPrefersTheSameRepository(t *testing.T) {
	t.Parallel()

	rows := []prepared.Review{
		{Repository: "acme/platform", Number: 904},
		{Repository: "kyleking/tlr", Number: 121},
		{Repository: "kyleking/tlr", Number: 118},
		{Repository: "kyleking/broken", Number: 7, Broken: "this file no longer parses"},
	}

	for _, c := range []struct {
		name string
		repo string
		was  int
		want string
	}{
		{"another in the repository just posted", "kyleking/tlr", 118, "kyleking/tlr#121"},
		{"nothing left in it", "acme/platform", 904, "kyleking/tlr#121"},
		{"a repository with nothing staged", "acme/other", 1, "acme/platform#904"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, ok := main.ReviewAfter(rows, c.repo, c.was)
			if !ok {
				t.Fatalf("nothing came next after %s#%d", c.repo, c.was)
			}

			if got != c.want {
				t.Errorf("next is %s, want %s", got, c.want)
			}
		})
	}

	// A review whose file no longer parses is not something to open, and one
	// staged review posted leaves nothing to move to.
	if _, ok := main.ReviewAfter(rows[3:], "kyleking/broken", 7); ok {
		t.Error("a review that cannot be read was offered as the next one")
	}
}
