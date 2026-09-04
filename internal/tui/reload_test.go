package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// An agent writing the artifact while the screen is open reaches the screen,
// rather than being clobbered by the next thing typed here.
func TestAReviewChangedOnDiskIsReloaded(t *testing.T) {
	t.Parallel()

	m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the finding"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	r.Find("c1").Turns = []artifact.Turn{{Author: "claude", Body: "Rewrote it."}}

	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	tick(t, m, path)

	frame := plain(m.Frame())

	for _, want := range []string{"the review changed on disk", "1 turn arrived", "Rewrote it."} {
		if !strings.Contains(frame, want) {
			t.Errorf("%q is missing after the reload:\n%s", want, frame)
		}
	}
}

// The collision the reload exists for: the comment being typed in was rewritten
// underneath. What is typed keeps the screen and the other version is offered,
// because throwing away either without asking is the bug.
func TestACommentRewrittenUnderTheCursorOffersBoth(t *testing.T) {
	t.Parallel()

	m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the finding"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	onto(t, m, "MAJOR")
	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	for _, r := range "mine" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	r.Find("c1").Body = "what the agent wrote"

	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	tick(t, m, path)

	frame := plain(m.Frame())

	if !strings.Contains(frame, "rewritten on disk while you were in it") {
		t.Errorf("the collision was not reported:\n%s", frame)
	}

	if !strings.Contains(frame, "mine") {
		t.Errorf("what was typed was lost:\n%s", frame)
	}

	press(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

	if frame = plain(m.Frame()); !strings.Contains(frame, "what the agent wrote") {
		t.Errorf("ctrl+t did not offer the other version:\n%s", frame)
	}

	press(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

	if frame = plain(m.Frame()); !strings.Contains(frame, "mine") {
		t.Errorf("ctrl+t did not put what was typed back:\n%s", frame)
	}
}

// tick delivers the watcher's message directly, since a test cannot wait on a
// timer without being slow and flaky about it.
func tick(t *testing.T, m *tui.Model, path string) {
	t.Helper()

	// The stamp has a clock in it, and a write inside the same tick as the one
	// the screen recorded would read as no change at all.
	time.Sleep(2 * time.Millisecond)

	m.Reload(path)
}
