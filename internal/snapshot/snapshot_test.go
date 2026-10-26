package snapshot_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/notice"
	"github.com/kyleking/second-look/internal/prstate"
	"github.com/kyleking/second-look/internal/snapshot"
)

var epoch = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func fixtureInbox() snapshot.Inbox {
	items := []inbox.PullRequest{{
		ID: "PR_kwDOinvented1", Repository: "example/widgets", Number: 7, Title: "fix: invented",
		Author: "someone", Labels: []string{"bug"}, Updated: epoch, Created: epoch, URL: "https://example.test/7",
	}}

	return snapshot.Inbox{
		Buckets: []snapshot.Bucket{{Name: "review requested", Items: snapshot.RowsOf(items)}},
		Facts: map[string]inbox.Record{
			"example/widgets#7": {
				Facts: inbox.Facts{Decision: "APPROVED", Checks: "PENDING"}, Updated: epoch, Fetched: epoch,
			},
		},
	}
}

func fixtureConversations() snapshot.Conversations {
	return snapshot.Conversations{
		Queue: conversations.Queue{Viewer: "reader", Conversations: []conversations.Conversation{{
			Kind: conversations.KindThread, Repository: "example/widgets", Number: 7, ThreadID: "PRRT_invented",
			Notes: []conversations.Note{{ID: 1, Author: "someone", Body: "invented", Created: epoch}},
		}}},
		Records: map[string]conversations.Record{
			"example/widgets#7": {Updated: epoch, Head: "abc", Fetched: epoch},
		},
		Notices: []notice.Thread{{ID: "1", Repo: "example/widgets", Number: 7, Updated: epoch}},
		Denied:  epoch,
	}
}

func fixtureStaged() snapshot.Staged {
	return snapshot.Staged{States: map[string]prstate.State{"example/widgets#7": {State: "OPEN", Mine: "COMMENTED"}}}
}

func sameJSON(t *testing.T, got, want any) {
	t.Helper()

	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}

	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(g, w) {
		t.Errorf("read back\n%s\nwant\n%s", g, w)
	}
}

func TestRoundTripAcrossProcessesAndPastTheMemoryTTL(t *testing.T) {
	t.Parallel()

	disk := cache.NewDiskCache(filepath.Join(t.TempDir(), "cache"))
	clk := &clock{at: epoch}

	writer := snapshot.Open(disk, clk.now)
	writer.SaveInbox("reader", fixtureInbox())
	writer.SaveConversations(fixtureConversations())
	writer.SaveStaged("reader", fixtureStaged())

	// A second store has empty memory, which is what the next run starts with.
	clk.at = epoch.Add(48 * time.Hour)
	reader := snapshot.Open(disk, clk.now)

	in, age, ok := reader.Inbox()
	if !ok || age != 48*time.Hour {
		t.Fatalf("inbox read past its TTL: ok=%v age=%v, want the stale value and its age", ok, age)
	}

	sameJSON(t, in.Buckets, fixtureInbox().Buckets)
	sameJSON(t, in.Facts, fixtureInbox().Facts)

	if got := snapshot.PullRequests(in.Buckets[0].Items)[0].ID; got != "PR_kwDOinvented1" {
		t.Errorf("the node id is %q after the round trip, want it kept", got)
	}

	conv, _, ok := reader.Conversations()
	if !ok {
		t.Fatal("conversations missing")
	}

	sameJSON(t, conv.Queue, fixtureConversations().Queue)
	sameJSON(t, conv.Notices, fixtureConversations().Notices)
	sameJSON(t, conv.Records, fixtureConversations().Records)

	within, past := epoch.Add(snapshot.DeniedFor-time.Minute), epoch.Add(snapshot.DeniedFor+time.Minute)
	if !conv.Refused(within) || conv.Refused(past) {
		t.Error("a refused stream is remembered for exactly a day")
	}

	st, _, ok := reader.Staged()
	if !ok {
		t.Fatal("staged missing")
	}

	sameJSON(t, st.States, fixtureStaged().States)

	clk.at = epoch.Add(9 * 24 * time.Hour)

	if _, _, ok := snapshot.Open(disk, clk.now).Inbox(); ok {
		t.Error("a snapshot past the week disk keeps an expired entry was served")
	}
}

func TestDropAndAccountSwitch(t *testing.T) {
	t.Parallel()

	disk := cache.NewDiskCache(filepath.Join(t.TempDir(), "cache"))
	clk := &clock{at: epoch}
	store := snapshot.Open(disk, clk.now)

	store.SaveInbox("reader", fixtureInbox())

	in, _, ok := store.Inbox()
	if !ok || !in.Owned("reader") || in.Owned("someone-else") {
		t.Fatalf("ownership: ok=%v viewer=%q, want it owned by reader alone", ok, in.Viewer)
	}

	store.DropInbox()

	if _, _, ok := store.Inbox(); ok {
		t.Error("a dropped snapshot was served from memory")
	}

	if _, _, ok := snapshot.Open(disk, clk.now).Inbox(); ok {
		t.Error("a dropped snapshot was served from disk")
	}
}

func TestSnapshotsAreOwnerOnly(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "cache")
	disk := cache.NewDiskCache(dir)

	snapshot.Open(disk, time.Now).SaveInbox("reader", fixtureInbox())

	for path, want := range map[string]os.FileMode{dir: 0o700, cache.DiskPath(disk, "queue"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s is %v, want %v", filepath.Base(path), got, want)
		}
	}
}
