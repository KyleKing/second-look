package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

func typed(t *testing.T, m *tui.Model, s string) {
	t.Helper()

	for _, r := range s {
		press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// The palette on : finds a key by what it does and runs it as the key would,
// then says the key, so the next time needs no palette.
func TestThePaletteRunsAKeyByWhatItDoes(t *testing.T) {
	t.Parallel()

	for _, width := range []int{80, 120} {
		m, _ := fixture(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})

		was := m.CursorRow()

		press(t, m, tea.KeyPressMsg{Code: ':', Text: ":"})
		typed(t, m, "hunk")

		if frame := plain(m.Frame()); !strings.Contains(frame, ": hunk") || !strings.Contains(frame, "commands") {
			t.Fatalf("%d: the palette is not up with the query:\n%s", width, frame)
		}

		press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

		frame := plain(m.Frame())
		if m.CursorRow() == was {
			t.Errorf("%d: running the hunk motion left the cursor on row %d:\n%s", width, was, frame)
		}

		if !strings.Contains(frame, "next time press ]h") {
			t.Errorf("%d: the run did not teach its key:\n%s", width, frame)
		}
	}
}

// A key the cursor gives nothing to act on says why, in the legend and when
// pressed, rather than being a silent dim.
func TestAGatedKeySaysWhy(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	press(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "m  no comment under the cursor; a writes one") {
		t.Errorf("m with no comment said nothing of why:\n%s", frame)
	}

	press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	typed(t, m, "/a state")

	if frame := plain(m.Frame()); !strings.Contains(frame, "no comment under the cursor") {
		t.Errorf("the legend does not give the reason m is off:\n%s", frame)
	}
}

// The legend explains a chord's key through the chord, and the drawer's
// explainer names its group.
func TestTheLegendExplainsAChordKey(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "z then…") {
		t.Fatalf("z did not wait for its second key:\n%s", frame)
	}

	press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "the line at · zz  the middle") {
		t.Errorf("zz was not explained through its chord:\n%s", frame)
	}

	if !strings.Contains(frame, "keys ·") {
		t.Errorf("the legend closed or ran the key:\n%s", frame)
	}
}

// The footer's status slot names the next step while one holds, and gives it
// up to a message.
func TestTheFooterNamesTheNextStep(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	lines := strings.Split(plain(m.Frame()), "\n")
	if foot := lines[len(lines)-1]; !strings.Contains(foot, "▸ next") {
		t.Errorf("the footer names no next step: %q", foot)
	}

	press(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})

	lines = strings.Split(plain(m.Frame()), "\n")
	if foot := lines[len(lines)-1]; strings.Contains(foot, "▸ next") {
		t.Errorf("the next step stayed over a message: %q", foot)
	}
}

// The band layout, from the palette's :next, names each step with its reason
// above the footer.
func TestTheNextBandFromThePalette(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "check err"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	press(t, m, tea.KeyPressMsg{Code: ':', Text: ":"})
	typed(t, m, "band above")
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	// The run says what it switched, and the band takes the footer once the
	// next key has cleared that.
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "── next") {
		t.Errorf("the band did not open:\n%s", frame)
	}

	if got := len(strings.Split(frame, "\n")); got != 30 {
		t.Errorf("the band changed the frame's height to %d", got)
	}
}

// An empty queue says what is empty and the keys out, rather than drawing a
// blank list.
func TestAnEmptyQueueSaysSo(t *testing.T) {
	t.Parallel()

	l := tui.NewTabs([]tui.Tab{{
		Name: "inbox", Sections: func() []tui.Section { return nil }, Columns: convColumns,
		State: func() tui.State { return tui.State{} },
	}}, 0)

	for _, size := range [][2]int{{80, 24}, {120, 30}} {
		l.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

		frame := plain(l.ListFrame())
		if !strings.Contains(frame, "Nothing in inbox") || !strings.Contains(frame, "? keys") {
			t.Errorf("%dx%d: an empty queue drew no card:\n%s", size[0], size[1], frame)
		}
	}
}
