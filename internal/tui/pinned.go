package tui

import "strings"

// pinned is the line a run of comments answers, held at the top of the frame
// while the run is read past it.
//
// A run taller than the frame scrolls its own anchor away, which leaves a
// screenful of prose about code nobody can see. It is drawn at the contrast of
// a hunk already read, so it reads as a heading rather than as a line of the
// diff that is somehow above the file.
func (m *Model) pinned(width int) (string, bool) {
	if m.offset == 0 || m.offset >= len(m.screen.rows) {
		return "", false
	}

	n := m.screen.rows[m.offset].run
	if n == 0 {
		return "", false
	}

	at, ok := m.anchorAbove(m.offset)
	if !ok {
		return "", false
	}

	text, _ := m.codeRow(m.screen.rows[at])
	word := strings.ToUpper(plural(n, "comment"))

	// The cursor column is a space here, since a pinned row is nowhere the
	// cursor can be.
	return " " + m.styles.behind.Render(fit(text, word, width-1)), true
}

// anchorAbove is the line of the diff a run hangs from, which is the nearest
// code row above it.
func (m *Model) anchorAbove(from int) (int, bool) {
	for i := from; i >= 0; i-- {
		if m.screen.rows[i].kind == rowCode {
			return i, true
		}
	}

	return 0, false
}

// fit puts the code on the left and what is said about it on the right, giving
// the code whatever room is left. A frame too narrow for both keeps the code,
// since the count is already on the run's own heading.
func fit(text, word string, width int) string {
	room := width - textWidth(word) - 1
	if room < textWidth(word) {
		return cut(text, width)
	}

	return pad(cut(text, room), room) + " " + word
}
