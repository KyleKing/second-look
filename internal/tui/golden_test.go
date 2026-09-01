package tui_test

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// The frames worth pinning are the ones a reader looks at longest and the two
// overlays that cover them. Color is stripped: every state carries a glyph as
// well as a color, so the glyph is what a monochrome terminal shows and what a
// golden file can hold without pinning a lipgloss version.
//
// These drive the model rather than a pty, because Frame is deterministic and a
// terminal is not: the pty tests in cmd/second-look cover what only a real
// terminal can show.
func TestFrames(t *testing.T) {
	t.Parallel()

	frames := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"review", nil},
		{"comment", []tea.KeyPressMsg{{Code: tea.KeyTab}}},
		{"help", []tea.KeyPressMsg{{Code: '?', Text: "?"}}},
		{"confirm", []tea.KeyPressMsg{
			{Code: 'm', Text: "m"}, {Code: 'x', Text: "x"}, {Code: 'S', Text: "S"},
		}},
		{"code", []tea.KeyPressMsg{{Code: 'c', Text: "c"}}},
		{"comments", []tea.KeyPressMsg{{Code: 'c', Text: "c"}, {Code: 'c', Text: "c"}}},
	}

	for _, width := range []int{80, 120} {
		for _, frame := range frames {
			t.Run(fmt.Sprintf("%s/%d", frame.name, width), func(t *testing.T) {
				t.Parallel()

				m := triaged(t)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})

				for _, k := range frame.keys {
					press(m, k)
				}

				golden.RequireEqual(t, []byte(plain(m.Frame())))
			})
		}
	}
}

// triaged is a review in every state the screen can show: one comment ready to
// post, one still a draft, and one skipped with its reason.
func triaged(t *testing.T) *tui.Model {
	t.Helper()

	ready := comment("ready", parsed, artifact.SideRight, 15, "split can fail now, so this has to check err.")

	draft := comment("draft", parsed, artifact.SideRight, 14, "Is first still 1 after the change?")
	draft.Status = artifact.StatusDraft
	draft.Severity = "question"
	draft.Note = "Unverified: I have not read the caller."

	skipped := comment("skipped", "internal/vcs/git.go", artifact.SideRight, 201, "Rename this to headSHA.")
	skipped.Status = artifact.StatusSkip
	skipped.Severity = "nit"
	skipped.SkipReason = "Out of scope for this change."

	m, _ := fixture(t, ready, draft, skipped)

	return m
}
