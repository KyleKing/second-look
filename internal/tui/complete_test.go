package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ctrl+n completes from what is already on this screen: the files the diff
// touches and the people who have said something on the pull request. Nothing
// here needs an index.
func TestCompletionOffersWhatTheReviewAlreadyKnows(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	onto(t, m, "REVIEW  no body")
	press(m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	for _, r := range "see diff.g" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	press(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "see diff.go") {
		t.Errorf("ctrl+n did not complete the file name:\n%s", frame)
	}

	// A stem with nothing behind it says so rather than doing nothing.
	for _, r := range " zzzz" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	press(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	if frame = plain(m.Frame()); !strings.Contains(frame, "nothing in this review starts with") {
		t.Errorf("a stem matching nothing said:\n%s", frame)
	}
}
