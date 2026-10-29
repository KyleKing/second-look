package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
)

// An agent replies by putting a real comment id in in_reply_to, and this is
// where it reads one. The ids are GitHub's, off the recording.
func TestShowThreadsNamesTheIDAReplyAddresses(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openOnlyCassette(t, sha))
	seedReview(t, dir, sha)
	seedThreads(t, dir, sha)

	res := runCLI(t, s, dir, "show", "2", "--threads")
	if res.code != 0 {
		t.Fatalf("show --threads failed: %s%s", res.stdout, res.stderr)
	}

	var open []struct {
		Path     string `json:"path"`
		Line     int    `json:"line"`
		Resolved bool   `json:"resolved"`
		ReplyTo  int64  `json:"reply_to"`
		Notes    []struct {
			ID int64 `json:"id"`
		} `json:"notes"`
	}

	if err := json.Unmarshal([]byte(res.stdout), &open); err != nil {
		t.Fatalf("reading the threads: %v\n%s", err, res.stdout)
	}

	if len(open) == 0 {
		t.Fatal("no threads were printed, so nothing here is exercised")
	}

	resolved := 0

	for i := range open {
		if open[i].ReplyTo != open[i].Notes[0].ID {
			t.Errorf("thread %d answers %d, but its first comment is %d",
				i, open[i].ReplyTo, open[i].Notes[0].ID)
		}

		if open[i].Path == "" || open[i].Line == 0 {
			t.Errorf("thread %d anchors nowhere: %+v", i, open[i])
		}

		if open[i].Resolved {
			resolved++
		}
	}

	// The recording carries one thread already resolved on GitHub, which must
	// still print, marked, rather than disappear once addressed.
	if resolved != 1 {
		t.Errorf("%d thread(s) printed as resolved, want 1", resolved)
	}
}

// Read-state has to outlive a force-push, which is the whole reason a mark is
// stored against a hunk's content rather than its position. The head moves and
// the diff is the same, so what was read is still read.
func TestGetCarriesReadHunksOntoANewHead(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openCassette(t, sha))
	seedReview(t, dir, fixtureHeadSHA)

	// Read everything against the head the fixture was staged at.
	seedDiff(t, dir)

	patch, err := artifact.LoadDiff(stored(t, dir), fixtureHeadSHA)
	if err != nil {
		t.Fatal(err)
	}

	refs := seen.Hunks(diff.Parse(patch))

	set, err := seen.Load(seen.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range refs {
		set.Mark(true, r.ID)
	}

	if err := seen.Save(seen.Path(stored(t, dir), 2), set, refs); err != nil {
		t.Fatal(err)
	}

	res := runCLI(t, s, dir, "get", "2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, "were already read") {
		t.Errorf("get did not say what carried:\n%s", res.stdout)
	}

	// The head moved, so the marks are only still there because they are keyed
	// by what the hunk says rather than by the commit it sat on.
	moved, err := artifact.LoadDiff(stored(t, dir), sha)
	if err != nil {
		t.Fatal(err)
	}

	after := seen.Hunks(diff.Parse(moved))

	back, err := seen.Load(seen.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatal(err)
	}

	if got := back.Count(after); got != len(after) {
		t.Errorf("%d of %d hunks stayed read across the head move", got, len(after))
	}
}

// TestGetPreparesTheReview is get standing on the head already, which is what
// `gh pr checkout` then `second-look get` looks like. Nothing moves; the
// artifact and the cached diff are written and the diff is the recorded bytes.
func TestGetPreparesTheReview(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openCassette(t, sha))

	res := runCLI(t, s, dir, "get", "2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(anonymize(t, res.stdout, sha, dir)))
	s.RequireAllPlayed(t)

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	if review.Owner != "KyleKing" || review.Repo != "second-look" || review.Number != 2 {
		t.Errorf("the review names %s/%s#%d, which is not the remote", review.Owner, review.Repo, review.Number)
	}

	// A review with no branches cannot be grouped into its stack, and a stack is
	// read bottom first, so getting several of them loses the order they go in.
	if review.HeadRef != headBranch || review.BaseRef != "main" {
		t.Errorf("the review joins %q to %q, want %q to main", review.HeadRef, review.BaseRef, headBranch)
	}

	// #nosec G304 -- a path under the test's own temporary directory
	cached, err := os.ReadFile(artifact.DiffPath(stored(t, dir), sha))
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
	s := ghcassette.Replay(t, openCassette(t, sha))

	// The fixture is staged against the recorded head; the pull request now
	// reports the scratch repository's.
	seedReview(t, dir, fixtureHeadSHA)

	res := runCLI(t, s, dir, "get", "2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	golden.RequireEqual(t, []byte(anonymize(t, res.stdout, sha, dir)))

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
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

// TestGetSkipsCheckoutOnAMergedPR is what get looks like once a pull request's
// branch is gone: GitHub still answers the diff and the threads by number, so
// there is no reason a missing branch should stop the refresh.
func TestGetSkipsCheckoutOnAMergedPR(t *testing.T) {
	t.Parallel()

	dir, _ := scratchRepo(t, "some-other-branch")
	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "merged", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = append(c.Interactions[:reads:reads], threadInteraction(t)...)
		c.Interactions[0].Stdout = strings.Replace(c.Interactions[0].Stdout, `"state":"OPEN"`, `"state":"MERGED"`, 1)
	}))

	res := runCLI(t, s, dir, "get", "2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, "is merged") {
		t.Errorf("get did not say the pull request was merged:\n%s", res.stdout)
	}

	if branch := strings.TrimSpace(inRepo(t, dir, "branch", "--show-current")); branch != "some-other-branch" {
		t.Errorf("the checkout moved to %q, want it left on some-other-branch", branch)
	}

	// #nosec G304 -- a path under the test's own temporary directory
	cached, err := os.ReadFile(artifact.DiffPath(stored(t, dir), fixtureHeadSHA))
	if err != nil {
		t.Fatalf("the cached diff: %v", err)
	}

	if !strings.Contains(string(cached), "+++ b/testdata/fixture/sample.go") {
		t.Error("the cached diff is not the one the recording carried")
	}
}

// TestGetRefusesToMoveADirtyTree is the guard that exists because moving the
// working copy is the only thing get does that can lose someone's work.
func TestGetRefusesToMoveADirtyTree(t *testing.T) {
	t.Parallel()

	dir, _ := scratchRepo(t, "some-other-branch")
	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "elsewhere", func(c *ghcassette.Cassette) {
		inCheckout(c)
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
		inCheckout(c)
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

// anonymize replaces the two things that differ on every run: the scratch
// repository's commit, and the temporary directory the store sits under.
func anonymize(t *testing.T, out, sha, dir string) string {
	t.Helper()

	out = strings.ReplaceAll(out, stored(t, dir), "<store>")

	return strings.ReplaceAll(out, sha[:7], "<head>")
}

// TestGetOffersToStashADirtyTree is the other half of the refusal above: a
// person at a terminal is asked, and answering yes parks the work rather than
// sending them away to do it by hand. The checkout that follows cannot land here
// (gh is replayed and the branch is unreachable), which is the case worth
// pinning anyway: a move that fails after the stash still says how to get the
// work back.
func TestGetOffersToStashADirtyTree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		answer string
		parked bool
	}{
		{name: "yes", answer: "y\r", parked: true},
		{name: "no", answer: "n\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir, _ := scratchRepo(t, "some-other-branch")
			s := ghcassette.Replay(t, deriveFrom(t, "post-review", "stash-"+tc.name, func(c *ghcassette.Cassette) {
				inCheckout(c)
				c.Interactions = c.Interactions[:1]
			}))

			if err := os.WriteFile(filepath.Join(dir, "unsaved.txt"), []byte("work\n"), 0o600); err != nil {
				t.Fatalf("dirtying the tree: %v", err)
			}

			sc := openReview(t, s, dir, "get", "2")
			sc.await("Stash them and check out #2?")
			sc.press(tc.answer)
			sc.wait()

			if parked := strings.Contains(inRepo(t, dir, "stash", "list"), "before reviewing #2"); parked != tc.parked {
				t.Errorf("parked=%v, want %v; the run wrote:\n%s", parked, tc.parked, sc.text())
			}

			dirty := inRepo(t, dir, "status", "--porcelain") != ""
			if dirty == tc.parked {
				t.Errorf("dirty=%v after answering %q", dirty, tc.answer)
			}

			if tc.parked && !strings.Contains(sc.text(), "git stash pop") {
				t.Errorf("the run never said how to get the work back:\n%s", sc.text())
			}
		})
	}
}

// A review staged against a head the pull request moved past used to end the
// open with a get command to type next. At a terminal it is a question
// instead: yes restages and opens in one step, no leaves the error it was.
func TestAStaleReviewOffersToRestage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		answer string
		opened bool
	}{
		{name: "yes", answer: "y\r", opened: true},
		{name: "no", answer: "n\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir, sha := scratchRepo(t, headBranch)
			s := ghcassette.Replay(t, staleOpenCassette(t, tc.name, sha))
			seedReview(t, dir, fixtureHeadSHA)

			sc := openReview(t, s, dir, "2")
			sc.await("Re-stage and open?")
			sc.press(tc.answer)

			if !tc.opened {
				refusedRestage(t, sc)

				return
			}

			openedAfterRestage(t, sc, s, dir, sha)
		})
	}
}

// staleOpenCassette is what offering the restage costs: the stale check, the
// get a yes runs, and the head check behind the opened frame each read the
// pull request. The restage reads as a detached target, so its calls carry
// --repo where the checkout's do not.
func staleOpenCassette(t *testing.T, name, sha string) string {
	t.Helper()

	return deriveFrom(t, "post-review", "stale-open-"+name, func(c *ghcassette.Cassette) {
		away := slices.Clone(c.Interactions[:reads])

		inCheckout(c)
		restamp(c, sha)

		for i := range away {
			away[i].Stdout = strings.ReplaceAll(away[i].Stdout, fixtureHeadSHA, sha)
		}

		first := slices.Clone(c.Interactions[:reads])
		c.Interactions = slices.Concat(
			[]ghcassette.Interaction{first[0], away[0], away[1]},
			threadInteraction(t),
			[]ghcassette.Interaction{first[0], commitsInteraction(nil, nil)},
		)
	})
}

func refusedRestage(t *testing.T, sc *screen) {
	t.Helper()

	if code := sc.wait(); code == 0 {
		t.Fatalf("a declined restage still opened:\n%s", sc.text())
	}
	if !strings.Contains(sc.text(), "staged against") {
		t.Errorf("the refusal lost the stale review's own error:\n%s", sc.text())
	}
}

func openedAfterRestage(t *testing.T, sc *screen, s *ghcassette.Session, dir, sha string) {
	t.Helper()

	sc.await("sample.go")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}
	if review.HeadSHA != sha {
		t.Errorf("the review is still staged against %s", review.HeadSHA)
	}

	s.RequireAllPlayed(t)
}

// inRepo reads the scratch repository back, so a test can check what moved
// rather than trusting what the run printed.
func inRepo(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- constants from the caller
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}

	return string(out)
}
