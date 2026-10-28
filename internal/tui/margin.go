package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pagePadX and pagePadY are the most margin the frame keeps from the
// terminal's edges, so text never bumps into the bezel. The margin yields
// before the page does: a screen at the minimum size has no room to give away.
const (
	pagePadX = 2
	pagePadY = 1
)

// pageSides is the count of page edges the margin falls on.
const pageSides = 2

// pagePad is the margin a terminal this size can carry while the page itself
// stays at or above the minimum.
func pagePad(width, height int) (int, int) {
	return min(pagePadX, max(0, (width-minWidth)/pageSides)),
		min(pagePadY, max(0, (height-minHeight)/pageSides))
}

// marginate insets every line of s by the page margin and leaves a blank row
// above the page.
func marginate(s string, padX, padY int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if padX > 0 {
			lines[i] = strings.Repeat(" ", padX) + l
		}
	}

	out := make([]string, 0, len(lines)+padY)
	for range padY {
		out = append(out, "")
	}

	return strings.Join(append(out, lines...), "\n")
}

// inset shifts a mouse position from screen cells into the page's own
// coordinates, which is what every layout and drawn rectangle is measured in.
func inset(at tea.Mouse, padX, padY int) tea.Mouse {
	at.X -= padX
	at.Y -= padY

	return at
}
