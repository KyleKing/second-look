package tui_test

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/framecheck"

	"github.com/kyleking/second-look/internal/tui"
)

// The goldens pin two widths of each frame; the faults they cannot see are the
// ones that show at the sizes nobody recorded. This draws the review and the
// queue with each of their overlays open at the sizes a terminal is opened at,
// and holds every frame to framecheck: no row past the screen's width, no box
// corner with an arm that joins nothing, no side broken partway down.
func TestEveryOverlayJoinsItsEdgesAtEverySize(t *testing.T) {
	t.Parallel()

	sizes := [][2]int{{80, 24}, {100, 30}, {120, 40}, {200, 60}}
	keys := map[string][]tea.KeyPressMsg{
		"review":   nil,
		"legend":   {key('?')},
		"palette":  {key(':'), key('h'), key('u')},
		"comment":  {{Code: tea.KeyTab}},
		"comments": {key('c')},
		"code":     {key('v')},
		"split":    {key('u'), key('v')},
		"confirm":  {key('m'), key('x'), key('S')},
		"prefix":   {key(']')},
	}

	for name, presses := range keys {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("review %s/%dx%d", name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				m := triaged(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range presses {
					press(t, m, k)
				}

				framecheck.Require(t, t.Name(), m.Frame(), framecheck.MaxWidth(size[0]))
			})
		}
	}

	lists := map[string][]tea.KeyPressMsg{
		"queue":  nil,
		"keys":   {key('?')},
		"filter": {key('/'), key('z'), key('z')},
	}

	for name, presses := range lists {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("queue %s/%dx%d", name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				l := list(t, nil)
				l.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range presses {
					l.Update(k)
				}

				framecheck.Require(t, t.Name(), l.ListFrame(), framecheck.MaxWidth(size[0]))
			})
		}
	}
}

// Text from GitHub carries emoji the terminal and lipgloss can measure apart:
// a VS16 heart, a ZWJ sequence, a text-default warning sign. A queue row holding
// them is settled before it is measured, so its right column stays put.
func TestEmojiInATitleKeepsTheRowsAligned(t *testing.T) {
	t.Parallel()

	sections := func() []tui.Section {
		return []tui.Section{{Name: "new since you looked", Rows: rowsOfConvs(
			convRow{
				key: "T1", repo: "kyleking/tlr", ref: "kyleking/tlr#118", anchor: "pool.go:42", age: "2h",
				author: "alice", status: "2 replies", title: "❤\U0000FE0F love it ⚠ but \U0001F469\U0000200D\U0001F4BB",
				unread: true,
			},
			convRow{
				key: "T2", repo: "kyleking/tlr", ref: "kyleking/tlr#119", anchor: "pool.go:50", age: "3h",
				author: "bob", status: "1 reply", title: "plain ascii title", unread: true,
			},
		)}}
	}

	l := tui.NewTabs([]tui.Tab{{Name: "conversations", Sections: sections, Columns: convColumns}}, 0)

	for _, width := range []int{80, 120} {
		l.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		framecheck.Require(t, fmt.Sprintf("queue at %d", width), l.ListFrame(), framecheck.MaxWidth(width))
	}
}

// TestChromeIsWidthStable holds every literal in the module to the glyph
// policy. The reactions are GitHub's own emoji, drawn as content; all seven
// are Unicode 6 emoji with emoji presentation, two cells in every width table
// and in tmux, and the heart, whose emoji form needs VS16, is drawn as ♡.
func TestChromeIsWidthStable(t *testing.T) {
	t.Parallel()

	framecheck.Source(t, "../..", framecheck.SkipFiles("internal/react/react.go"))
}
