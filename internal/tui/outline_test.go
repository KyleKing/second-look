package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

// A change past sixty files opens on its outline. Enter reads the selected
// chapter from its heading, and I comes back to the same selection.
func TestABigChangeOpensOnItsOutline(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, manyFiles(70))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if got := plain(m.Frame()); !strings.Contains(got, "CHAPTERS · 1 of 3") {
		t.Fatalf("seventy files did not open on the outline:\n%s", got)
	} else if !strings.Contains(got, "⇢ alpha  ") || !strings.Contains(got, " "+strings.Repeat("─", 12)+" ") {
		t.Errorf("twenty-four hunks are not folded into a twelve cell strip beside the name:\n%s", got)
	}

	pressKey(t, m, 'j')
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := m.CursorText(); !strings.Contains(got, "beta") {
		t.Errorf("enter on the second chapter landed on %q, want its heading", got)
	}

	pressKey(t, m, 'I')

	if got := plain(m.Frame()); !strings.Contains(got, "CHAPTERS · 2 of 3") {
		t.Errorf("I did not come back to the chapter it left:\n%s", got)
	}

	if mode := m.View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Errorf("the outline did not capture the mouse: %v", mode)
	}

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})

	if got := plain(m.Frame()); !strings.Contains(got, "CHAPTERS · 3 of 3") {
		t.Errorf("the wheel did not move the selection:\n%s", got)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if got := plain(m.Frame()); strings.Contains(got, "CHAPTERS ·") {
		t.Errorf("esc left the outline up:\n%s", got)
	}
}

// The outline is the first screen of a big change, and the view keys still
// answer from it: t leaves for the conversations, c for what will post, and v
// for the code, since a key eaten there reads as a view that does not work.
func TestTheOutlineLeavesForTheViews(t *testing.T) {
	t.Parallel()

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}, manyFiles(70), tui.WithThreads([]threads.Thread{
		finding("x", 15, "doggo", "wrap it"),
	}))

	if got := plain(m.Frame()); !strings.Contains(got, "CHAPTERS · 1 of 3") {
		t.Fatalf("the big change did not open on its outline:\n%s", got)
	}

	pressKey(t, m, 't')

	got := plain(m.Frame())
	if strings.Contains(got, "CHAPTERS ·") {
		t.Fatalf("t left the outline up:\n%s", got)
	}
	if !strings.Contains(got, "wrap it") {
		t.Fatalf("t did not land on the conversations:\n%s", got)
	}

	pressKey(t, m, 'I')
	pressKey(t, m, 'c')

	if got = plain(m.Frame()); !strings.Contains(got, "no comments staged") {
		t.Errorf("c did not land on what will post:\n%s", got)
	}
}
