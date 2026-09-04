package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
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
	m.SetRounds(func(string) (*diff.Diff, error) { return diff.Parse([]byte(earlier)), nil })
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	pressKey(m, 'H')

	if got := plain(m.Frame()); !strings.Contains(got, short(was)) {
		t.Fatalf("H did not offer the earlier round:\n%s", got)
	}

	pressKey(m, '1')

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
	pressKey(m, 'H')

	if got := plain(m.Frame()); !strings.Contains(got, "return head") {
		t.Errorf("H did not put the whole diff back:\n%s", got)
	}
}

func short(sha string) string { return sha[:7] }
