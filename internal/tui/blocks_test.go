package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
)

// Three blocks separated by blank rows read as three comments on three
// consecutive lines, and the line they all answer scrolls off the top before
// the last of them is read. So a run says how many it holds and which line, and
// each comment in it says where it sits.
func TestARunOfCommentsSaysItIsOne(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t,
		comment("c1", parsed, artifact.SideRight, 15, "the first finding"),
		comment("c2", parsed, artifact.SideRight, 15, "the second finding"),
		comment("c3", parsed, artifact.SideRight, 18, "somewhere else"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})

	frame := plain(m.Frame())

	for _, want := range []string{"2 COMMENTS ON LINE 15", "(1 of 2)", "(2 of 2)"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the run never said %q:\n%s", want, frame)
		}
	}

	// A comment standing alone is not a run, so it is numbered against nothing.
	if strings.Contains(frame, "(1 of 1)") || strings.Contains(frame, "ON LINE 18") {
		t.Errorf("a comment on its own was drawn as a run:\n%s", frame)
	}
}
