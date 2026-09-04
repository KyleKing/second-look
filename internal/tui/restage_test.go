package tui_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// The head after a push landed mid-read. It drops the second file and rewords
// the line the staged comment anchors to.
const pushed = `diff --git a/internal/vcs/diff.go b/internal/vcs/diff.go
index 1111111..5555555 100644
--- a/internal/vcs/diff.go
+++ b/internal/vcs/diff.go
@@ -14,6 +14,7 @@ func Parse(r io.Reader) ([]Hunk, error) {
 	first := 1
-	lines := split(r)
+	lines, err := splitAll(r)
+	if err != nil {
 	last := 4
`

// A review read over twenty minutes outlives the answer given when it started,
// and finding out at submit time that the head moved is finding out too late.
// So ctrl+r takes the new head in place rather than sending the reader to the
// shell, and says what the move cost.
func TestRestagingTakesAHeadThatMoved(t *testing.T) {
	t.Parallel()

	m, path, _ := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	fresh := reviewAt(t, path)
	fresh.HeadSHA = "9f2c4d1"

	m.SetRestage(func(context.Context) (*tui.Restaged, error) {
		return &tui.Restaged{
			Review: fresh, Diff: diff.Parse([]byte(pushed)), HeadSHA: fresh.HeadSHA,
		}, nil
	})

	// Nothing to restage until the head has actually moved, because a key that
	// spends three API calls on an unchanged head is a key nobody presses.
	press(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})

	if got := plain(m.Frame()); !strings.Contains(got, "nothing to restage") {
		t.Errorf("restaging an unmoved head did not say so:\n%s", got)
	}

	m.SawHead("9f2c4d1")
	press(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "splitAll(r)") {
		t.Errorf("the new diff never reached the screen:\n%s", frame)
	}

	if strings.Contains(frame, "head moved to") {
		t.Errorf("the title still reports a head that has been taken:\n%s", frame)
	}

	if !strings.Contains(frame, "restaged against 9f2c4d1") {
		t.Errorf("the restage did not say what it landed on:\n%s", frame)
	}
}

var errRateLimited = errors.New("gh: rate limited")

// A restage that failed leaves the diff on screen as good as it was, and says
// why rather than emptying the frame.
func TestARestageThatFailedKeepsTheDiff(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	m.SetRestage(func(context.Context) (*tui.Restaged, error) {
		return nil, errRateLimited
	})
	m.SawHead("9f2c4d1")

	press(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "could not restage: gh: rate limited") {
		t.Errorf("the failure was not reported:\n%s", frame)
	}

	if !strings.Contains(frame, "check err") {
		t.Errorf("the review was lost with the failed restage:\n%s", frame)
	}
}
