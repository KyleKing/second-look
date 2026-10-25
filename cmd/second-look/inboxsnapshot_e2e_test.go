package main_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kyleking/aragonite/cache"
	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/snapshot"
)

// cacheIn is where a run with this home keeps its cache, by the rule os follows.
func cacheIn(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches", "second-look")
	}

	return filepath.Join(home, ".cache", "second-look")
}

func snapshotsIn(home string, now func() time.Time) *snapshot.Store {
	return snapshot.Open(cache.NewDiskCache(cacheIn(home)), now)
}

// gated puts a gh in front of the stand-in that answers nothing until the file
// it returns exists, so a test can look at the screen while every call is out.
func gated(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	open := filepath.Join(dir, "open")
	script := "#!/bin/sh\nwhile [ ! -e \"$GATE\" ]; do sleep 0.02; done\nexec \"$STAND_IN_GH\" \"$@\"\n"

	//nolint:gosec // it has to run
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil {
		t.Fatalf("writing the gated gh: %v", err)
	}

	return dir + string(os.PathListSeparator) + os.Getenv("PATH"), open
}

// The first frame comes from disk: the rows of the last open are on screen
// while every gh call is still out, and what the searches then answer replaces
// them. The snapshot was read as another login, so the read that names this
// one drops it and writes its own.
func TestInboxPaintsTheLastQueueBeforeGHAnswers(t *testing.T) {
	t.Parallel()

	home := quietHome(t)
	ago := time.Now().Add(-3 * time.Hour)

	seeded := snapshot.Inbox{
		Buckets: []snapshot.Bucket{{Name: "review requested", Items: snapshot.RowsOf([]inbox.PullRequest{{
			ID: "PR_invented", Repository: "example/cached", Number: 9, Title: "feat: a row from the last open",
			Author: "someone", Created: ago, Updated: ago,
		}})}},
		Facts: map[string]inbox.Record{"example/cached#9": {Updated: ago, Fetched: ago}},
	}
	snapshotsIn(home, func() time.Time { return ago }).SaveInbox("someone-else", seeded)

	cassette := filepath.Join(t.TempDir(), "inbox.golden")
	if err := ghcassette.Save(cassette, onScreen(t)); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	s := ghcassette.Replay(t, cassette)
	path, release := gated(t)

	sc := openReview(t, s, t.TempDir(),
		"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache",
		"PATH="+path, "GATE="+release, "STAND_IN_GH="+s.GH(), "inbox")

	sc.await("a row from the last open")
	sc.await("cached 3h ago")

	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	sc.await("aragonite#100")
	sc.await("approved")
	sc.press("q")
	sc.wait()

	got, _, ok := snapshotsIn(home, time.Now).Inbox()
	if !ok || !got.Owned("kyleking") {
		t.Fatalf("the snapshot after the refresh is ok=%v viewer=%q, want it rewritten for the login the read named",
			ok, got.Viewer)
	}

	for _, b := range got.Buckets {
		for _, r := range b.Items {
			if r.Repository == "example/cached" {
				t.Errorf("the snapshot still holds %s#%d from the other login", r.Repository, r.Number)
			}
		}
	}

	if _, kept := got.Facts["example/cached#9"]; kept {
		t.Error("the snapshot still holds a fact read as the other login")
	}
}
