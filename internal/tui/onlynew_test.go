package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// U narrows the review to what this pass has not read, which is the second look
// at a pull request that moved under the first. The test is the read mark, so a
// hunk that survived a force-push unchanged stays hidden.
func TestOnlyNewHidesWhatIsAlreadyRead(t *testing.T) {
	t.Parallel()

	m := readable(t, patch)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Mark the first hunk read, which is what an earlier pass leaves behind.
	press(m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	press(m, tea.KeyPressMsg{Code: ' ', Text: " "})

	const readHunk = "lines, err := split(r)"

	if before := plain(m.Frame()); !strings.Contains(before, readHunk) {
		t.Fatalf("the hunk to be hidden is not on screen:\n%s", before)
	}

	press(m, tea.KeyPressMsg{Code: 'U', Text: "U"})

	after := plain(m.Frame())

	if !strings.Contains(after, "showing only what is new") {
		t.Fatalf("U said nothing about narrowing:\n%s", after)
	}

	if !strings.Contains(after, "already read") {
		t.Errorf("the hidden hunks are not accounted for:\n%s", after)
	}

	if strings.Contains(after, readHunk) {
		t.Errorf("the hunk already read is still drawn:\n%s", after)
	}

	press(m, tea.KeyPressMsg{Code: 'U', Text: "U"})

	back := plain(m.Frame())

	if !strings.Contains(back, "showing every hunk again") {
		t.Errorf("U did not come back:\n%s", back)
	}

	if !strings.Contains(back, readHunk) {
		t.Errorf("the hunk did not come back:\n%s", back)
	}
}
