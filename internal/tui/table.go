package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// tableLines draws a GitHub table as aligned columns: the | --- | divider row
// is dropped, every cell pads to the widest in its column, and a table wider
// than the room loses from its widest column rather than letting one cell
// squeeze all the rest.
func tableLines(lines []string, room int) []string {
	var rows [][]string

	for _, l := range lines {
		if cells := tableCells(l); cells != nil {
			rows = append(rows, cells)
		}
	}

	if len(rows) == 0 {
		return lines
	}

	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}

	widths := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}

	for total := sum(widths) + (cols-1)*len(tableDivider); total > room && cols > 1; total-- {
		widest := slices.Index(widths, slices.Max(widths))
		if widths[widest] <= minColumn {
			break
		}

		widths[widest]--
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		parts := make([]string, cols)
		for i := range cols {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}

			parts[i] = cell + strings.Repeat(" ", max(0, widths[i]-lipgloss.Width(cell)))
		}

		out = append(out, cut(strings.TrimRight(strings.Join(parts, tableDivider), " "), room))
	}

	return out
}

const (
	tableDivider = " │ "
	minColumn    = 3
)

// tableCells reads a | a | b | row into its cells, and answers nil for the
// | --- | divider GitHub writes under the head so it is spent rather than
// drawn.
func tableCells(line string) []string {
	text := strings.TrimSpace(line)
	if !strings.HasPrefix(text, "|") {
		return nil
	}

	cells := strings.Split(strings.Trim(text, "|"), "|")

	divider := true
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
		if strings.Trim(cells[i], ":- ") != "" {
			divider = false
		}
	}

	if divider {
		return nil
	}

	return cells
}

func sum(ns []int) int {
	total := 0
	for _, n := range ns {
		total += n
	}

	return total
}
