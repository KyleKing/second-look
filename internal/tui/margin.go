package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pagePadX is the most margin the frame keeps from the terminal's sides, so
// text never bumps into the bezel. The margin yields before the page does: a
// screen at the minimum width has no room to give away.
const pagePadX = 2

// pageSides is the count of page edges the margin falls on.
const pageSides = 2

// pagePad is the margin a terminal this wide can carry while the page itself
// stays at or above the minimum.
func pagePad(width int) int {
	return min(pagePadX, max(0, (width-minWidth)/pageSides))
}

// marginate insets every line of s by the page margin.
func marginate(s string, padX int) string {
	if padX == 0 {
		return s
	}

	pad := strings.Repeat(" ", padX)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}

	return strings.Join(lines, "\n")
}

// inset shifts a mouse position from screen cells into the page's own
// coordinates, which is what every layout and drawn rectangle is measured in.
func inset(at tea.Mouse, padX int) tea.Mouse {
	at.X -= padX

	return at
}
