package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// searched is the rows each of the cassette's searches answered, in the order
// the queue's buckets run, with the node ids a facts read asks by.
func searched(t *testing.T, c *ghcassette.Cassette) []snapshot.Bucket {
	t.Helper()

	var out []snapshot.Bucket

	for i, in := range c.Interactions {
		if in.Args[0] != "search" {
			continue
		}

		//nolint:tagliatelle // gh answers in camelCase and these names are its own
		var raw []struct {
			ID         string `json:"id"`
			Repository struct {
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"repository"`
			Number    int       `json:"number"`
			Title     string    `json:"title"`
			UpdatedAt time.Time `json:"updatedAt"`
		}

		if err := json.Unmarshal([]byte(in.Stdout), &raw); err != nil {
			t.Fatalf("reading search %d: %v", i, err)
		}

		b := snapshot.Bucket{Name: inbox.Plan(30)[len(out)].Name}

		for _, r := range raw {
			b.Items = append(b.Items, snapshot.Row{
				ID: r.ID, Repository: r.Repository.NameWithOwner, Number: r.Number,
				Title: r.Title, Updated: r.UpdatedAt,
			})
		}

		out = append(out, b)
	}

	return out
}

func rowKey(r *snapshot.Row) string { return fmt.Sprintf("%s#%d", r.Repository, r.Number) }

// withheld is a facts record for every open row, as the last open read them a
// minute ago, then three of them spoiled: one that moved since, one read too
// long ago, and one with checks still running. It returns the spoiled rows'
// node ids.
func withheld(
	t *testing.T, buckets []snapshot.Bucket, facts map[string]inbox.Facts,
) (map[string]inbox.Record, []string) {
	t.Helper()

	now := time.Now()
	records := map[string]inbox.Record{}

	var open []snapshot.Row

	for _, b := range buckets[:len(buckets)-1] {
		open = append(open, b.Items...)
	}

	if len(open) < 4 {
		t.Fatalf("the recording has %d open rows, want at least four to leave one untouched", len(open))
	}

	for i := range open {
		r := &open[i]
		records[rowKey(r)] = inbox.Record{Facts: facts[rowKey(r)], Updated: r.Updated, Fetched: now.Add(-time.Minute)}
	}

	moved, aged, running := &open[0], &open[1], &open[2]

	rec := records[rowKey(moved)]
	rec.Updated = rec.Updated.Add(-time.Hour)
	records[rowKey(moved)] = rec

	rec = records[rowKey(aged)]
	rec.Fetched = now.Add(-inbox.MaxAge - time.Minute)
	records[rowKey(aged)] = rec

	rec = records[rowKey(running)]
	rec.Facts.Checks = "PENDING"
	records[rowKey(running)] = rec

	return records, []string{moved.ID, aged.ID, running.ID}
}

// A snapshot that still holds is not read again: only the rows that are new,
// moved, had checks running, or were read too long ago go in the facts call.
func TestInboxFetchesOnlyWhatTheSnapshotCannotVouchFor(t *testing.T) {
	t.Parallel()

	base := onScreen(t)
	buckets := searched(t, base)

	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "inbox", "testdata", "facts.json"))
	if err != nil {
		t.Fatal(err)
	}

	read, err := inbox.DecodeFacts(body)
	if err != nil {
		t.Fatal(err)
	}

	records, want := withheld(t, buckets, read.Facts)

	home := quietHome(t)
	snapshotsIn(home, time.Now).SaveInbox("kyleking", snapshot.Inbox{Buckets: buckets, Facts: records})

	base.Interactions = slices.DeleteFunc(base.Interactions, func(in ghcassette.Interaction) bool {
		return slices.Equal(in.Args[:2], []string{"api", "graphql"})
	})
	base.Interactions = append(base.Interactions, ghcassette.Interaction{
		Args: inbox.FactsArgs(want), Stdout: string(body),
	})

	cassette := filepath.Join(t.TempDir(), "inbox.golden")
	if err := ghcassette.Save(cassette, base); err != nil {
		t.Fatalf("writing the derived cassette: %v", err)
	}

	s := ghcassette.Replay(t, cassette)
	sc := openReview(t, s, t.TempDir(),
		"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache", "inbox")

	sc.await("aragonite#100")
	sc.awaitPlayed(s, len(base.Interactions))
	sc.press("q")
	sc.wait()

	s.RequireAllPlayed(t)
}
