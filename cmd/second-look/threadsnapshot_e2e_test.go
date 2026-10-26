package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/notice"
	"github.com/kyleking/second-look/internal/resolve"
	"github.com/kyleking/second-look/internal/snapshot"
)

// say rewrites the first pull request comment of the pull request id names,
// which is how a test tells what a read fetched from what it kept.
func (q queueFixture) say(t *testing.T, id, body string) {
	t.Helper()

	for i := range q.nodes {
		if q.nodes[i].ID != id {
			continue
		}

		var comments struct {
			Nodes []map[string]any `json:"nodes"`
		}

		if err := json.Unmarshal(q.nodes[i].fields["comments"], &comments); err != nil || len(comments.Nodes) == 0 {
			t.Fatalf("the fixture's pull request %s has no comments to rewrite: %v", id, err)
		}

		comments.Nodes[0]["body"] = body

		raw, err := json.Marshal(comments)
		if err != nil {
			t.Fatal(err)
		}

		q.nodes[i].fields["comments"] = raw
	}
}

// key is how a read names the pull request id names.
func (q queueFixture) key(id string) string {
	for i := range q.nodes {
		if q.nodes[i].ID == id {
			return conversations.KeyOf(q.nodes[i].Repository.NameWithOwner, q.nodes[i].Number)
		}
	}

	return ""
}

// seedQueue saves the fixture's queue as the snapshot a run opened at saved
// would find, each pull request read at saved as well.
func seedQueue(t *testing.T, home string, q queueFixture, saved time.Time) {
	t.Helper()

	read, err := conversations.Decode([]byte(q.heavy(t, q.ids()...)))
	if err != nil {
		t.Fatalf("decoding the queue fixture: %v", err)
	}

	updated, err := time.Parse(time.RFC3339, fixtureUpdated)
	if err != nil {
		t.Fatal(err)
	}

	records := map[string]conversations.Record{}
	for _, id := range q.ids() {
		records[q.key(id)] = conversations.Record{
			Updated: updated, Head: strings.Repeat("a", fixtureHeadLen), Fetched: saved,
		}
	}

	snapshotsIn(home, func() time.Time { return saved }).SaveConversations(snapshot.Conversations{
		Queue: *read, Records: records,
	})
}

// The first frame comes from disk, and the read behind it fetches only the pull
// request that moved: the light read names both, and the heavy one is asked
// about one. A call for the other would find no answer in the cassette.
func TestConversationsPaintTheLastQueueAndFetchOnlyWhatMoved(t *testing.T) {
	t.Parallel()

	home := quietHome(t)
	q := readQueueFixture(t)
	moved := q.ids()[1]
	saved := time.Now().Add(-3 * time.Minute)

	seedQueue(t, home, q, saved)

	q.say(t, moved, "cc @me, now reworded")

	cassette := filepath.Join(t.TempDir(), "threads.golden")
	err := ghcassette.Save(cassette, &ghcassette.Cassette{Interactions: []ghcassette.Interaction{
		{
			Args:   conversations.LightArgs(conversations.DefaultLimit),
			Stdout: q.light(t, map[string]string{moved: "2026-08-02T00:00:00Z"}),
		},
		{Args: conversations.HeavyArgs([]string{moved}), Stdout: q.heavy(t, moved)},
		{Args: notice.Args(), Stdout: "[]"},
	}})
	if err != nil {
		t.Fatalf("writing the cassette: %v", err)
	}

	s := ghcassette.Replay(t, cassette)
	path, release := gated(t)

	sc := openReview(t, s, t.TempDir(),
		"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache",
		"PATH="+path, "GATE="+release, "STAND_IN_GH="+s.GH(), "threads")

	sc.await("this leaks")
	sc.await("cached 3m ago")
	sc.await("cc @me for the diff parser")

	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	sc.await(", now reworded")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the queue exited %d:\n%s", code, sc.text())
	}

	s.RequireAllPlayed(t)

	got, _, ok := snapshotsIn(home, time.Now).Conversations()
	if !ok {
		t.Fatal("the refresh left no snapshot")
	}

	if rec := got.Records[q.key(moved)]; !rec.Updated.Equal(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the moved pull request was recorded as read at %v", rec.Updated)
	}

	if rec := got.Records[q.key(q.ids()[0])]; !rec.Fetched.Equal(saved) {
		t.Errorf("the pull request that did not move was read again: fetched %v, want %v", rec.Fetched, saved)
	}
}

// A token without the notifications scope gets a 404 for the stream every time.
// The first run learns that and says so quietly, and the next asks nobody: its
// cassette holds no notifications call, so one would fail the run.
func TestARefusedNotificationStreamIsNotAskedForAgainWithinADay(t *testing.T) {
	t.Parallel()

	home := quietHome(t)
	q := readQueueFixture(t)

	const refused = "not available to this token"

	run := func(interactions ...ghcassette.Interaction) {
		t.Helper()

		cassette := filepath.Join(t.TempDir(), "threads.golden")
		if err := ghcassette.Save(cassette, &ghcassette.Cassette{Interactions: interactions}); err != nil {
			t.Fatalf("writing the cassette: %v", err)
		}

		s := ghcassette.Replay(t, cassette)
		sc := openReview(t, s, t.TempDir(),
			"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache", "threads")

		sc.await(refused)
		sc.await("this leaks")
		sc.press("q")

		if code := sc.wait(); code != 0 {
			t.Fatalf("the queue exited %d:\n%s", code, sc.text())
		}

		s.RequireAllPlayed(t)
	}

	light := ghcassette.Interaction{Args: conversations.LightArgs(conversations.DefaultLimit), Stdout: q.light(t, nil)}
	heavy := ghcassette.Interaction{Args: conversations.HeavyArgs(q.ids()), Stdout: q.heavy(t, q.ids()...)}

	run(light, heavy, ghcassette.Interaction{
		Args: notice.Args(), Stderr: "gh: Not Found (HTTP 404)", Exit: 1,
	})
	run(light)
}

// Resolving a thread takes it out of what the next open paints, and drops the
// pull request's record so the next read fetches it again rather than trusting
// what it held.
func TestResolvingAThreadLeavesItOutOfTheNextOpen(t *testing.T) {
	t.Parallel()

	home := quietHome(t)
	q := readQueueFixture(t)

	read, err := conversations.Decode([]byte(q.heavy(t, q.ids()...)))
	if err != nil {
		t.Fatalf("decoding the queue fixture: %v", err)
	}

	var leaks *conversations.Conversation

	for i := range read.Conversations {
		if read.Conversations[i].Anchor() == "a.go:12" {
			leaks = &read.Conversations[i]
		}
	}

	if leaks == nil {
		t.Fatal("the fixture holds no thread at a.go:12")
	}

	interactions := append(queueInteractions(t),
		ghcassette.Interaction{Args: resolve.ThreadArgs(leaks.ThreadID)},
		ghcassette.Interaction{Args: resolve.ReactArgs(leaks.First().NodeID)},
	)

	cassette := filepath.Join(t.TempDir(), "threads.golden")
	if err := ghcassette.Save(cassette, &ghcassette.Cassette{Interactions: interactions}); err != nil {
		t.Fatalf("writing the cassette: %v", err)
	}

	s := ghcassette.Replay(t, cassette)
	sc := openReview(t, s, t.TempDir(),
		"HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache", "threads")

	sc.await("this leaks")
	sc.press("/this leaks")
	sc.press("\r")
	sc.press("R")
	sc.await("resolved")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the queue exited %d:\n%s", code, sc.text())
	}

	got, _, ok := snapshotsIn(home, time.Now).Conversations()
	if !ok {
		t.Fatal("the run left no snapshot")
	}

	for i := range got.Queue.Conversations {
		if got.Queue.Conversations[i].ThreadID == leaks.ThreadID {
			t.Error("the snapshot still holds the thread that was resolved")
		}
	}

	if _, held := got.Records[conversations.KeyOf(leaks.Repository, leaks.Number)]; held {
		t.Error("the snapshot still vouches for the pull request the thread was resolved on")
	}

	if len(got.Queue.Conversations) == 0 {
		t.Error("resolving one thread emptied the snapshot")
	}
}
