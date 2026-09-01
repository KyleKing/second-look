package conversations_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/conversations"
)

// queue.json is written rather than recorded. A real reply carries the private
// repository names, logins, and titles of whatever is open, and this repository
// is public; the shapes in it are GitHub's own with invented content.
func fixture(t *testing.T) *conversations.Queue {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", "queue.json"))
	if err != nil {
		t.Fatal(err)
	}

	q, err := conversations.Decode(body)
	if err != nil {
		t.Fatalf("decoding the queue: %v", err)
	}

	return q
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}

	return t
}

// Every rule that admits or drops a conversation is checked against one reply,
// because they interact: a bot's inline thread stays while the same bot's pull
// request comment goes, and that only reads as a rule when both are present.
func TestDecodeKeepsOnlyTheConversationsThatAreYours(t *testing.T) {
	t.Parallel()

	q := fixture(t)

	if q.Viewer != "me" {
		t.Fatalf("viewer is %q, want me", q.Viewer)
	}

	type row struct {
		kind conversations.Kind
		key  string
	}

	got := make(map[row]bool, len(q.Conversations))
	for i := range q.Conversations {
		c := &q.Conversations[i]
		got[row{kind: c.Kind, key: c.Key()}] = true
	}

	kept := []struct {
		name string
		row  row
	}{
		{"a bot's inline thread, which is anchored and resolvable", row{conversations.KindThread, "T-bot-open"}},
		{"a thread you spoke in on someone else's pull request", row{conversations.KindThread, "T-i-spoke"}},
		{"a thread you answered last", row{conversations.KindThread, "T-i-answered"}},
		{"a thread that mentions you", row{conversations.KindThread, "T-mentions-me"}},
		{
			"a person's comment on your pull request",
			row{conversations.KindComment, "o/mine#1:comment:22"},
		},
		{
			"a person's review body on your pull request",
			row{conversations.KindReview, "o/mine#1:review:32"},
		},
		{
			"a comment mentioning you on someone else's pull request",
			row{conversations.KindComment, "o/theirs#2:comment:51"},
		},
	}

	for _, want := range kept {
		if !got[want.row] {
			t.Errorf("dropped %s: %+v", want.name, want.row)
		}
	}

	dropped := []struct {
		name string
		row  row
	}{
		{"a resolved thread", row{conversations.KindThread, "T-resolved"}},
		{"a thread that is nothing to do with you", row{conversations.KindThread, "T-not-mine"}},
		{"a bot's pull request comment", row{conversations.KindComment, "o/mine#1:comment:21"}},
		{"your own comment, which nobody owes an answer to", row{conversations.KindComment, "o/mine#1:comment:23"}},
		{"a bot's review body", row{conversations.KindReview, "o/mine#1:review:31"}},
		{"a review nobody has submitted", row{conversations.KindReview, "o/mine#1:review:33"}},
		{"an approval with no body", row{conversations.KindReview, "o/mine#1:review:34"}},
		{"a comment you already thumbs-upped", row{conversations.KindComment, "o/theirs#2:comment:52"}},
		{"a thread you thumbs-upped without resolving", row{conversations.KindThread, "T-thumbed"}},
	}

	for _, unwanted := range dropped {
		if got[unwanted.row] {
			t.Errorf("kept %s: %+v", unwanted.name, unwanted.row)
		}
	}

	if len(got) != len(kept) {
		t.Errorf("kept %d conversations, want %d: %+v", len(got), len(kept), got)
	}
}

// A thread carries what a reply needs, and the other two surfaces carry nothing,
// because GitHub gives them no threaded reply to hang from.
func TestReplyToOnlyAnswersForAThread(t *testing.T) {
	t.Parallel()

	for _, c := range fixture(t).Conversations {
		got := c.ReplyTo()

		if c.Kind == conversations.KindThread {
			if got == 0 {
				t.Errorf("%s carries no comment id, so nothing can answer it", c.Key())
			}

			continue
		}

		if got != 0 {
			t.Errorf("%s offers a reply id of %d on a surface that takes none", c.Key(), got)
		}
	}
}

// The buckets turn on two things and nothing else: whether the last word was
// yours, and whether you have looked since the last word arrived.
func TestBucketsSplitOnTheLastWordAndWhenYouLooked(t *testing.T) {
	t.Parallel()

	q := fixture(t)
	looked := conversations.NewLooked()

	find := func(key string) *conversations.Conversation {
		t.Helper()

		for i := range q.Conversations {
			if q.Conversations[i].Key() == key {
				return &q.Conversations[i]
			}
		}

		t.Fatalf("the fixture no longer carries %s", key)

		return nil
	}

	// Read one as it stands, and one as it stood before its last comment landed.
	read := find("T-bot-open")
	looked.Mark(read, read.Updated())

	stale := find("T-mentions-me")
	looked.Mark(stale, stale.Updated().Add(-time.Hour))

	where := map[string]string{}

	for _, b := range conversations.Buckets(q, looked) {
		for i := range b.Items {
			where[b.Items[i].Key()] = b.Name
		}
	}

	cases := []struct {
		key  string
		want string
		why  string
	}{
		{"T-bot-open", conversations.BucketWaiting, "read as it stands, and still nobody has answered it"},
		{"T-mentions-me", conversations.BucketNew, "looked at before its last comment arrived"},
		{"o/mine#1:comment:22", conversations.BucketNew, "never looked at"},
		{"T-i-spoke", conversations.BucketNew, "you asked and the answer arrived while you were away"},
		{"T-i-answered", conversations.BucketAwaiting, "you had the last word"},
	}

	for _, c := range cases {
		if got := where[c.key]; got != c.want {
			t.Errorf("%s (%s) is in %q, want %q", c.key, c.why, got, c.want)
		}
	}

	if n := conversations.Count(conversations.Buckets(q, looked)); n != len(q.Conversations) {
		t.Errorf("the buckets hold %d conversations and the queue has %d", n, len(q.Conversations))
	}
}

// A mark survives a restart, and one for a conversation the queue no longer
// carries is dropped rather than kept forever.
func TestLookedSurvivesARestartAndForgetsWhatIsGone(t *testing.T) {
	t.Parallel()

	q := fixture(t)
	path := filepath.Join(t.TempDir(), "conversations.toml")
	now := at("2026-09-01T12:00:00Z")

	looked := conversations.NewLooked()
	for i := range q.Conversations {
		looked.Mark(&q.Conversations[i], now)
	}

	live := q.Conversations[:1]
	if err := conversations.SaveLooked(path, looked, live); err != nil {
		t.Fatal(err)
	}

	back, err := conversations.LoadLooked(path)
	if err != nil {
		t.Fatal(err)
	}

	if !back.Since(&live[0]) {
		t.Errorf("%s came back unread", live[0].Key())
	}

	for i := 1; i < len(q.Conversations); i++ {
		if back.Since(&q.Conversations[i]) {
			t.Errorf("%s was pruned from the queue and its mark was kept", q.Conversations[i].Key())
		}
	}
}

// A missing file is an empty set, so the first run reports every conversation
// as new rather than failing.
func TestLoadLookedTreatsAMissingFileAsNothingRead(t *testing.T) {
	t.Parallel()

	looked, err := conversations.LoadLooked(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}

	q := fixture(t)
	if looked.Since(&q.Conversations[0]) {
		t.Error("nothing has been read and a conversation reports otherwise")
	}
}

// The queue is read by scanning a column, so the columns line up within a
// bucket, and every row carries the line that says whether it is worth opening.
func TestWriteAlignsAndQuotesTheLastWord(t *testing.T) {
	t.Parallel()

	now := at("2026-09-01T12:00:00Z")
	buckets := []conversations.Bucket{{
		Name: conversations.BucketNew,
		Items: []conversations.Conversation{
			{
				Kind: conversations.KindThread, Repository: "a/b", Number: 7, Title: "short",
				ThreadID: "T1", Path: "x.go", Line: 3,
				Notes: []conversations.Note{{
					Author:  "alice",
					Body:    "  \n\ngood catch, pushed a defer\nmore",
					Created: now.Add(-90 * time.Minute),
				}},
			},
			{
				Kind: conversations.KindReview, Repository: "much/longer-repo", Number: 14691, Title: "long",
				Notes: []conversations.Note{
					{Author: "someone", Body: "", Created: now.Add(-3 * time.Hour)},
					{Author: "bob", Body: "", Created: now.Add(-2 * time.Hour)},
				},
			},
		},
	}}

	var b strings.Builder
	if err := conversations.Write(&b, buckets, now); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("wrote %d lines, want a heading and two rows of two:\n%s", len(lines), b.String())
	}

	if !strings.HasPrefix(lines[0], conversations.BucketNew+" (2)") {
		t.Errorf("the heading does not carry the count: %q", lines[0])
	}

	if got, want := strings.Index(lines[1], "1h"), strings.Index(lines[3], "2h"); got != want {
		t.Errorf("the age column does not line up: %d and %d\n%s", got, want, b.String())
	}

	if !strings.Contains(lines[2], "good catch, pushed a defer") {
		t.Errorf("the row does not quote what was said: %q", lines[2])
	}

	if strings.Contains(lines[2], "more") {
		t.Errorf("the row quotes past the first line: %q", lines[2])
	}

	if !strings.Contains(lines[4], "(no text)") {
		t.Errorf("a comment with no body reads as blank rather than saying so: %q", lines[4])
	}

	if !strings.Contains(lines[3], "review body") {
		t.Errorf("a review body does not say where it sits: %q", lines[3])
	}
}
