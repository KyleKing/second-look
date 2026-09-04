package tui

import (
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
)

// selection is the range V opened: the row it opened on, and the file and side
// that row belongs to. A range that has since wandered into another file, or
// across to the other side of the diff, anchors nowhere GitHub would take it,
// so both are kept to be checked rather than assumed.
type selection struct {
	path, side string
	line, row  int
}

// askRange opens a range on the line under the cursor, and a second press drops
// it. There is no other way out, since esc leaves the screen.
func (m *Model) askRange() {
	if m.selected != nil {
		m.selected = nil
		m.say("range dropped", false)

		return
	}

	if !m.onCode() {
		m.say("V opens a range on a line of the diff, and this row is not one", false)

		return
	}

	r := m.screen.rows[m.cursor]
	a := anchorOf(r.path, r.line)
	m.selected = &selection{path: a.path, side: a.side, line: a.line, row: m.cursor}

	m.say("range open on "+a.path+":"+strconv.Itoa(a.line)+"; move, then a or s", false)
}

// ranged is the lines the range covers, and false when there is no range or the
// cursor has left the file and side it opened on.
func (m *Model) ranged() (int, int, bool) {
	if m.selected == nil || !m.onCode() {
		return 0, 0, false
	}

	r := m.screen.rows[m.cursor]

	a := anchorOf(r.path, r.line)
	if a.path != m.selected.path || a.side != m.selected.side {
		return 0, 0, false
	}

	return min(m.selected.line, a.line), max(m.selected.line, a.line), true
}

// inRange reports whether a row is one the open range covers, which is what the
// cursor column says about it.
func (m *Model) inRange(i int) bool {
	if m.selected == nil || i == m.cursor {
		return false
	}

	lo, hi := min(m.selected.row, m.cursor), max(m.selected.row, m.cursor)

	return i >= lo && i <= hi
}

// rangeWord is what a comment about to be written covers, said in its title.
func rangeWord(path string, from, to int) string {
	if from == to {
		return path + ":" + strconv.Itoa(to)
	}

	return path + ":" + strconv.Itoa(from) + "-" + strconv.Itoa(to)
}

// rangeText is the file's own text for every line a suggestion would replace,
// which is what its editor opens on.
func (m *Model) rangeText(path, side string, from, to int) (string, bool) {
	lines := make([]string, 0, to-from+1)

	for line := from; line <= to; line++ {
		text, ok := m.diff.Anchor(path, side, line)
		if !ok {
			return "", false
		}

		lines = append(lines, text)
	}

	return strings.Join(lines, "\n"), true
}

// firstOf is the line a comment opens on: its start where it covers a range,
// and the line itself where it covers one line.
func firstOf(start, line int) int {
	if start == 0 {
		return line
	}

	return start
}

// takeRange is the range the cursor and the mark cover, closed by the taking:
// a range is answered by the key that uses it, so the next comment is not
// quietly written against the last one's lines.
//
// A range of one line has no start, since GitHub reads start_line as the
// opening of a range and a start equal to the end is a range of nothing.
func (m *Model) takeRange(fallback diff.Line, path string) (int, int, string) {
	a := anchorOf(path, fallback)

	from, to, ok := m.ranged()
	if !ok {
		return 0, a.line, a.side
	}

	m.selected = nil

	if from == to {
		return 0, to, a.side
	}

	return from, to, a.side
}
