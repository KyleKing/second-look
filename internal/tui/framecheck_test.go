package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/kyleking/aragonite/tui/framecheck"

	"github.com/kyleking/second-look/internal/artifact"
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

// A card scrolled up to the bottom edge used to draw just its top border on the
// last body row, a box with nothing in it whose corners reached into the footer,
// and scrolled past the top, just its foot on the first. Every scroll position
// from the top to past the card is held to framecheck, the body as a viewport so
// a card the scroll cuts across reads as clipped.
func TestACardsTopEdgeNeverSitsAloneOnTheLastRow(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			m, _, _ := fixtureWith(t, tallPatch(80),
				comment("c1", parsed, artifact.SideRight, 40, "a card that scrolls into the bottom edge"))
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

			for step := range 60 {
				top, bottom := m.BodyRows()
				framecheck.Require(t, fmt.Sprintf("after %d scrolls", step), m.Frame(),
					framecheck.MaxWidth(size[0]), framecheck.Viewport(top, bottom))
				m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			}
		})
	}
}

// Code is drawn as it is written, so an emoji in a code line is not settled the
// way GitHub text is. The row is measured with the width the renderer and tmux
// give each cluster (lipgloss's grapheme widths, which Bubble Tea's cell buffer
// and tmux 3.4 agree on for all of these), so the scrollbar keeps its column on
// a row holding one in every look: plain, rich, side by side, and wrapped.
func TestAnEmojiInACodeLineKeepsTheScrollbarsColumn(t *testing.T) {
	t.Parallel()

	patch := emojiPatch()

	looks := map[string][]tea.KeyPressMsg{
		"rich":  nil,
		"plain": {key('u'), key('g')},
		"split": {key('u'), key('s')},
		"wrap":  {key('u'), key('g'), key('u'), key('w')},
	}

	for name, presses := range looks {
		for _, width := range []int{80, 140} {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				t.Parallel()

				m, _, _ := fixtureWith(t, patch)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})

				for _, k := range presses {
					press(t, m, k)
				}

				sameColumn(t, m, width)
			})
		}
	}
}

// emojiPatch is a hunk whose code lines each hold one cluster of a class the
// width tables disagree on, with a wide and a plain line among them.
func emojiPatch() string {
	emoji := []string{"🩺", "🟠", "❤\uFE0F", "👩\u200d💻", "👍🏽", "🇺🇸", "⚠", "\u4e2d", "x"}

	var b strings.Builder

	b.WriteString("diff --git a/" + parsed + " b/" + parsed + "\n--- a/" + parsed + "\n+++ b/" + parsed + "\n")
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", 3*len(emoji), 3*len(emoji))

	for i := range 3 * len(emoji) {
		fmt.Fprintf(&b, " s := \"%s doctor\" // %d, a line long enough to wrap at the narrow width\n",
			emoji[i%len(emoji)], i)
	}

	return b.String()
}

// sameColumn holds every body line of m's frame to the first one's width, which
// is where the scrollbar sits.
func sameColumn(t *testing.T, m *tui.Model, width int) {
	t.Helper()

	top, bottom := m.BodyRows()
	lines := strings.Split(m.Frame(), "\n")

	want := xansi.StringWidth(lines[top])
	if want < width-2 {
		t.Fatalf("the first body line is %d cells of %d", want, width)
	}

	for i := top; i <= bottom && i < len(lines); i++ {
		if w := xansi.StringWidth(lines[i]); w != want {
			t.Errorf("body line %d is %d cells, so its scrollbar is off column %d:\n%s",
				i, w, want, framecheck.Ruled(m.Frame()))
		}
	}
}
