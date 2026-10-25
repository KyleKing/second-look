package tui_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

// earlier is the diff as it read at the round before this one: the second file
// unchanged, and the first not in it at all.
const earlier = `diff --git a/internal/vcs/git.go b/internal/vcs/git.go
index 3333333..4444444 100644
--- a/internal/vcs/git.go
+++ b/internal/vcs/git.go
@@ -200,3 +201,3 @@ func Head() string {
 	return head
`

// A review read over three pushes is read against the head it is on, and what
// is worth a second pass is what has moved since a round already read. H picks
// the round and hides every hunk that round already carried, which is U with
// the round named by hand.
func TestComparingAgainstAnEarlierRoundHidesWhatItAlreadyCarried(t *testing.T) {
	t.Parallel()

	const was = "6bc1218809a6faf83bc266c7a10b6b096f814a74"

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Rounds: []artifact.Round{
			{SHA: was, Staged: time.Now().Add(-24 * time.Hour)},
			{SHA: "a1b2c3d", Staged: time.Now()},
		},
	}, patch)
	m.SetRounds(func(context.Context, string) (*diff.Diff, error) { return diff.Parse([]byte(earlier)), nil })
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	pressKey(t, m, 'H')

	if got := plain(m.Frame()); !strings.Contains(got, short(was)) {
		t.Fatalf("H did not offer the earlier round:\n%s", got)
	}

	pressKey(t, m, '1')

	frame := plain(m.Frame())
	if strings.Contains(frame, "return head") {
		t.Errorf("a hunk the earlier round already carried is still drawn:\n%s", frame)
	}

	// The file keeps its heading and says what it is holding back, since a file
	// that vanished would read as a file the push deleted.
	if !strings.Contains(frame, "unchanged since "+short(was)) {
		t.Errorf("nothing said why the hunk went:\n%s", frame)
	}

	if !strings.Contains(frame, "lines, err := split(r)") {
		t.Errorf("a hunk that arrived since the earlier round was hidden:\n%s", frame)
	}

	// A second H puts the whole diff back, since a filter with no way out is a
	// filter nobody turns on.
	pressKey(t, m, 'H')

	if got := plain(m.Frame()); !strings.Contains(got, "return head") {
		t.Errorf("H did not put the whole diff back:\n%s", got)
	}
}

// body builds one hunk of a.go whose third line reads what the caller gives it.
func body(third string) string {
	return "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,9 +1,9 @@\n one\n two\n-3\n+" +
		third + "\n four\n five\n six\n seven\n eight\n nine\n"
}

// A todo is answered where the lines it hangs from changed since the round, or
// where its thread names the commit that fixed it. The box says the same of each
// round, with who pushed it, so the one to compare against can be picked.
func TestComparingAgainstARoundSaysWhichTodosWereAnswered(t *testing.T) {
	t.Parallel()

	const (
		was   = "6bc1218809a6faf83bc266c7a10b6b096f814a74"
		fixer = "b2c3d4e5f60718293a4b5c6d7e8f901234567890"
		named = "9b8e7d6"
	)

	now := time.Now()
	month := now.Add(-30 * 24 * time.Hour)
	review := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Rounds: []artifact.Round{
			{SHA: was, Staged: month},
			{SHA: fixer, Staged: now.Add(-2 * time.Hour)},
			{SHA: "a1b2c3d", Staged: now},
		},
	}

	open := make([]threads.Thread, 0, 3)

	for i, tc := range []struct {
		line int
		note string
	}{
		{3, "will do"}, {8, "will do"}, {8, "Fixed in " + named + "."},
	} {
		id := int64(100 + i)
		open = append(open, threads.Thread{
			ID: "PRRT_" + strconv.Itoa(i), Path: "a.go", Side: artifact.SideRight, Line: tc.line,
			Notes: []threads.Note{
				{ID: id, Author: "doggo", Body: "check this"},
				{ID: id + 1000, Author: "fixer", Body: tc.note},
			},
		})
		review.Comments = append(review.Comments, artifact.Comment{
			ID: "reply-" + strconv.FormatInt(id, 10), Path: "a.go", Side: artifact.SideRight, Line: tc.line,
			InReplyTo: id, Severity: "major", Status: artifact.StatusTodo, Body: "will fix",
		})
	}

	commits := []plan.Commit{
		{ID: was, Author: "watch-doggo"},
		{ID: fixer, Author: "watch-doggo", Hunks: []order.Ref{{Path: "a.go", Hunk: 1}}},
		{ID: "a1b2c3d", Author: "watch-doggo"},
	}

	m, _, _ := modelFor(t, review, body("THREE"), tui.WithThreads(open), tui.WithCommits(commits, ""))
	m.SetRounds(func(_ context.Context, sha string) (*diff.Diff, error) {
		if sha == was {
			return diff.Parse([]byte(body("tree"))), nil
		}

		return diff.Parse([]byte(body("THREE"))), nil
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	reads, ok := m.StartRoundReads()().(tea.BatchMsg)
	if !ok {
		t.Fatal("H read the earlier rounds as one command, so the box has nothing to wait on")
	}

	for _, read := range reads {
		m.Update(read())
	}

	pressKey(t, m, 'H')

	box := plain(m.Frame())
	for _, want := range []string{
		fmt.Sprintf("1  %s  %-6s  watch-doggo · 1 commit · 2 of 3 todos answered", short(was), month.Format("Jan 2")),
		"2  " + short(fixer) + "  2h ago  watch-doggo · 1 commit · 1 of 3 todos answered",
	} {
		if !strings.Contains(box, want) {
			t.Errorf("the H box never said %q:\n%s", want, box)
		}
	}

	pressKey(t, m, '1')

	frame := plain(m.Frame())
	for _, want := range []string{
		"since round 1 · 2 of 3 todos answered",
		"answered in " + short(fixer), "not answered", "answered in " + named,
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("comparing against round 1 never said %q:\n%s", want, frame)
		}
	}
}

func short(sha string) string { return sha[:7] }
