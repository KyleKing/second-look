package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A change past sixty files opens on its outline. Enter reads the selected
// chapter from its heading, and I comes back to the same selection.
func TestABigChangeOpensOnItsOutline(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, manyFiles(70))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if got := plain(m.Frame()); !strings.Contains(got, "CHAPTERS · 1 of 3") {
		t.Fatalf("seventy files did not open on the outline:\n%s", got)
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

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if got := plain(m.Frame()); strings.Contains(got, "CHAPTERS ·") {
		t.Errorf("esc left the outline up:\n%s", got)
	}
}
