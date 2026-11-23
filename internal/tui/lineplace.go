package tui

import "fmt"

// linePlace is where the cursor stands in terms a different diff can still answer:
// the file, the line on the side that numbers it, what that line says, and how
// far down the frame it was drawn. A rebuild against the same diff keeps the
// row it was on by what the row is, which a push breaks, since a push renumbers
// the lines below every insertion. So a place falls back on the line's text.
type linePlace struct {
	path string
	side string
	text string
	// id is the staged comment the cursor is in, by id, because a comment's
	// index moves the moment one before it posts.
	id string
	// within is how many rows into that comment's block the cursor was.
	within int
	line   int
	// row is the cursor's distance from the top of the frame, which is what
	// keeps the eye where it was when everything around the line moved.
	row int
	// top marks a place that, found nowhere, goes back to the top rather than
	// staying at an index that now means something else.
	top bool
}

// linePlaceHere is where the cursor stands now. A row that is not a line of code
// (a comment, a thread, a gap) is placed by the line it hangs from, which is
// the nearest line above it in the same file.
func (m *Model) linePlaceHere() linePlace {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return linePlace{}
	}

	r := &m.screen.rows[m.cursor]
	p := linePlace{path: r.path, row: m.cursor - m.offset}

	if r.comment >= 0 && r.comment < len(m.review.Comments) {
		p.id = m.review.Comments[r.comment].ID

		for i := m.cursor - 1; i >= 0 && m.screen.rows[i].comment == r.comment; i-- {
			p.within++
		}
	}

	for i := m.cursor; i >= 0; i-- {
		above := &m.screen.rows[i]
		if above.kind == rowFile || (p.path != "" && above.path != p.path) {
			break
		}

		if a, ok := lineOf(above); ok {
			p.path, p.side, p.line, p.text = a.path, a.side, a.line, above.line.Text

			break
		}
	}

	return p
}

// lineOf is the anchor of a row drawn from a line of the diff, and false for
// every other row.
func lineOf(r *row) (anchor, bool) {
	if r.kind != rowCode || r.around || (r.line.Old == 0 && r.line.New == 0) {
		return anchor{}, false
	}

	return anchorOf(r.path, r.line), true
}

// holdPlace has the next rebuild put the cursor back on the line it stands on
// now, at the same height on the frame.
func (m *Model) holdPlace() {
	p := m.linePlaceHere()
	m.holdAt = &p
}

// landBack lands the cursor on a place after a rebuild, and says what it could
// not keep. The staged comment it was in answers first, then the line by its
// number where the text still matches, then the line by its text alone, then
// the nearest line drawn of the same file, then the file's heading.
func (m *Model) landBack(p linePlace) string {
	at, note := m.findPlace(p)
	if at < 0 {
		m.landAtTop(p)

		return note
	}

	m.landAt(at, p.row)

	return note
}

// findPlace is the row a place lands on, -1 where it lands nowhere, and what
// the landing could not keep.
func (m *Model) findPlace(p linePlace) (int, string) {
	// The rebuild already kept the row where it could tell it was the same one,
	// and that row may be inside a comment block, which is more than its first.
	if m.standsOn(p) {
		return m.cursor, ""
	}

	if i := m.stagedRow(p.id); i >= 0 {
		if k := i + p.within; k < len(m.screen.rows) && m.screen.rows[k].comment == m.screen.rows[i].comment {
			return k, ""
		}

		return i, ""
	}

	if p.path == "" {
		return -1, ""
	}

	if i, note := m.findLine(p); i >= 0 {
		return i, note
	}

	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.kind == rowFile && r.path == p.path {
			if p.line > 0 {
				return i, "nothing of " + p.path + " is drawn now; on its heading"
			}

			return i, ""
		}
	}

	return -1, p.path + " is not in this diff"
}

// findLine is the row drawing a place's line: by its number where the text
// still matches, by its text where the number moved, and otherwise the nearest
// line of the same file still drawn, which it says.
func (m *Model) findLine(p linePlace) (int, string) {
	if p.line == 0 {
		return -1, ""
	}

	if i := m.lineRow(p.path, p.side, p.line, p.text); i >= 0 {
		return i, ""
	}

	// The line moved but did not change: something above it in the file was
	// inserted or dropped, and its text says where it went.
	if n, ok := m.diff.Relocate(p.path, p.side, p.text); ok {
		if i := m.lineRow(p.path, p.side, n, p.text); i >= 0 {
			return i, ""
		}
	}

	if i := m.nearestLine(p); i >= 0 {
		return i, fmt.Sprintf("%s:%d is not drawn now; on the nearest hunk", p.path, p.line)
	}

	return -1, ""
}

// standsOn reports whether the cursor's row is the place: inside the same
// staged comment, or on the same line reading the same.
func (m *Model) standsOn(p linePlace) bool {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return false
	}

	r := &m.screen.rows[m.cursor]

	if p.id != "" {
		return r.comment >= 0 && r.comment < len(m.review.Comments) && m.review.Comments[r.comment].ID == p.id
	}

	a, ok := lineOf(r)

	return ok && p.line > 0 && a == anchor{path: p.path, side: p.side, line: p.line} && r.line.Text == p.text
}

func (m *Model) landAtTop(p linePlace) {
	if p.top {
		m.cursor = 0
		m.reveal()
	}
}

// landAt puts the cursor on row i, drawn row lines down the frame where the
// frame can scroll that far.
func (m *Model) landAt(i, row int) {
	m.cursor = i
	m.offset = clamp(i-row, m.maxOffset())
}

// stagedRow is the first row of the staged comment named id, or -1.
func (m *Model) stagedRow(id string) int {
	if id == "" {
		return -1
	}

	for i := range m.review.Comments {
		if m.review.Comments[i].ID != id {
			continue
		}

		for j := range m.screen.rows {
			if m.screen.rows[j].comment == i {
				return j
			}
		}
	}

	return -1
}

// lineRow is the row drawing the line numbered n on side of path, reading text,
// or -1.
func (m *Model) lineRow(path, side string, n int, text string) int {
	for i := range m.screen.rows {
		r := &m.screen.rows[i]
		if a, ok := lineOf(r); ok && a.path == path && a.side == side && a.line == n && r.line.Text == text {
			return i
		}
	}

	return -1
}

// nearestLine is the drawn line of p's file whose number comes closest to
// p's, which is a line of the nearest hunk still drawn, or -1 where none of the
// file's hunks is.
func (m *Model) nearestLine(p linePlace) int {
	best, gap := -1, -1

	for i := range m.screen.rows {
		r := &m.screen.rows[i]
		if r.path != p.path || r.hunk == 0 {
			continue
		}

		a, ok := lineOf(r)
		if !ok {
			continue
		}

		n := a.line
		if a.side != p.side {
			n = numberOn(r.line, p.side)
		}

		if n == 0 {
			continue
		}

		if d := abs(n - p.line); gap < 0 || d < gap {
			best, gap = i, d
		}
	}

	return best
}

// joined puts what a landing could not keep ahead of what the screen already
// says, since a cursor that moved on its own is the news a reader needs first.
func joined(said, note string) string {
	if said == "" {
		return note
	}

	return note + "; " + said
}
