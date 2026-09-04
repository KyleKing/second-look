package prepared_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/prepared"
)

// older is a head this pull request was pushed past. Nothing reads a cache
// keyed by it once the review has moved on.
const (
	head  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	older = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// cachedRoot stages a checkout holding one review at head and the caches of two
// heads, which is what a pull request pushed to twice leaves behind.
func cachedRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "second-look",
		Number: 42, HeadSHA: head, Event: artifact.EventComment,
	}
	if err := artifact.Save(artifact.Path(root, 42), r); err != nil {
		t.Fatal(err)
	}

	for _, sha := range []string{head, older} {
		if err := artifact.SaveDiff(root, sha, []byte("a patch")); err != nil {
			t.Fatal(err)
		}

		if err := artifact.SaveThreads(root, sha, []string{}); err != nil {
			t.Fatal(err)
		}

		if err := artifact.SaveScore(root, sha, artifact.Cost{Total: 3}); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func exists(t *testing.T, path string) bool {
	t.Helper()

	_, err := os.Stat(path)

	return err == nil
}

// Nothing else collects these files, so a pull request pushed to a dozen times
// would keep a dozen copies of its diff for as long as the checkout lives.
func TestSweepDropsTheCachesOfEveryHeadNoReviewIsStagedAgainst(t *testing.T) {
	t.Parallel()

	root := cachedRoot(t)

	n, err := prepared.Sweep(root)
	if err != nil {
		t.Fatal(err)
	}

	if n != 3 {
		t.Errorf("removed %d files, want the diff, threads, and rating of the older head", n)
	}

	for _, path := range []string{
		artifact.DiffPath(root, older), artifact.ThreadsPath(root, older), artifact.ScorePath(root, older),
	} {
		if exists(t, path) {
			t.Errorf("%s survived the sweep", path)
		}
	}

	for _, path := range []string{
		artifact.DiffPath(root, head), artifact.ThreadsPath(root, head), artifact.ScorePath(root, head),
	} {
		if !exists(t, path) {
			t.Errorf("%s was swept, and the staged review is pinned to it", path)
		}
	}
}

// Comparing against an earlier round reads the diff cached at it, so a round
// the review still lists is a round the sweep has to leave alone. They all go
// when the review does, so nothing outlives what it is for.
func TestSweepKeepsEveryRoundAReviewWasReadAt(t *testing.T) {
	t.Parallel()

	root := cachedRoot(t)

	path := artifact.Path(root, 42)

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	r.Rounds = []artifact.Round{{SHA: older, Staged: time.Now()}, {SHA: head, Staged: time.Now()}}
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	n, err := prepared.Sweep(root)
	if err != nil {
		t.Fatal(err)
	}

	if n != 0 {
		t.Errorf("removed %d files, and both heads are rounds the review was read at", n)
	}

	if !exists(t, artifact.DiffPath(root, older)) {
		t.Error("the diff of a round the review was read at was swept")
	}
}

// A review that will not parse has no head to compare against, so sweeping
// around it would delete the one diff a hand repair needs.
func TestSweepLeavesEverythingWhileAReviewIsUnreadable(t *testing.T) {
	t.Parallel()

	root := cachedRoot(t)

	if err := os.WriteFile(artifact.Path(root, 7), []byte("version = 1\nowner = \"broken\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := prepared.Sweep(root)
	if err != nil {
		t.Fatal(err)
	}

	if n != 0 {
		t.Errorf("swept %d files past a review that does not parse", n)
	}
}

// Discarding is the only way a staged review leaves the disk without posting,
// so what it leaves behind is what accumulates.
func TestDiscardTakesTheReviewItsReadMarksAndItsCaches(t *testing.T) {
	t.Parallel()

	root := cachedRoot(t)
	marks := filepath.Join(root, artifact.Dir, "seen", "pr-42.toml")

	if err := os.MkdirAll(filepath.Dir(marks), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(marks, []byte("[[hunk]]\nid = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rows, err := prepared.List(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := prepared.Discard(&rows[0]); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		artifact.Path(root, 42), marks,
		artifact.DiffPath(root, head), artifact.ThreadsPath(root, head), artifact.ScorePath(root, head),
		artifact.DiffPath(root, older),
	} {
		if exists(t, path) {
			t.Errorf("%s survived the discard", path)
		}
	}
}
