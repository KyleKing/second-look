package tui_test

import (
	"fmt"
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

// The line a run answers used to scroll away before the last of them was read,
// which leaves a screenful of prose about code nobody can see. So it is pinned
// to the top of the frame for as long as the run is being read past it.
func TestALongRunKeepsTheLineItAnswersInView(t *testing.T) {
	t.Parallel()

	const many = 6

	cs := make([]artifact.Comment, 0, many)
	for i := range many {
		cs = append(cs, comment(fmt.Sprintf("c%d", i), parsed, artifact.SideRight, 15,
			"a finding long enough to take a few rows of a narrow frame, said again and again"))
	}

	m, _ := fixture(t, cs...)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})

	go2(m, ']', 'c')

	for range 20 {
		pressKey(m, 'j')
	}

	top := strings.SplitN(plain(m.Frame()), "\n", 3)[1]
	if !strings.Contains(top, "lines, err := split(r)") || !strings.Contains(top, "COMMENTS") {
		t.Errorf("the line the run answers is not pinned:\n%s", plain(m.Frame()))
	}
}

// A skip is a decision with a reason against it, so it is counted rather than
// dropped. Inside a run it costs a live comment's room, which is the one place
// that matters, so there it gathers below them until somebody asks.
func TestSkipsInARunGatherBelowTheLiveOnes(t *testing.T) {
	t.Parallel()

	live := comment("c1", parsed, artifact.SideRight, 15, "this one posts")

	skipped := comment("c2", parsed, artifact.SideRight, 15, "the skipped body")
	skipped.Status, skipped.SkipReason = artifact.StatusSkip, "not this change"

	m, _ := fixture(t, live, skipped)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "1 comment skipped here") || strings.Contains(frame, "the skipped body") {
		t.Fatalf("the skip was not gathered:\n%s", frame)
	}

	go2(m, ']', 'c')
	go2(m, ']', 'c')
	go2(m, 'z', 'a')

	if got := plain(m.Frame()); !strings.Contains(got, "the skipped body") {
		t.Errorf("za did not open the gathered skip:\n%s", got)
	}
}
