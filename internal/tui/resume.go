package tui

// Resume is where each tab was left, for a screen that closes to open a review
// and is built again afterwards.
type Resume struct {
	tabs []tabMark
}

type tabMark struct {
	key    string
	cursor int
	offset int
	query  string
}

// placed reports a mark that names somewhere, as against a tab nobody has
// looked at, whose rows arrive later and must not be pinned to the first.
func (m tabMark) placed() bool { return m.key != "" || m.cursor > 0 }

// Where is each tab's place, for handing to Restore.
func (l *List) Where() Resume {
	l.remember()

	out := Resume{tabs: make([]tabMark, len(l.views))}

	for i := range l.views {
		v := &l.views[i]
		out.tabs[i] = tabMark{cursor: v.cursor, offset: v.offset, query: v.filter.query}
	}

	// Only the tab being read has rows built, so only its cursor can be named.
	if len(out.tabs) > 0 {
		if row := l.current(); row != nil {
			out.tabs[l.at].key = row.Key
		}
	}

	return out
}

// Restore puts each tab back where Where left it. A zero Resume changes
// nothing, which is what the first pass of a session gets.
func (l *List) Restore(r Resume) {
	if len(r.tabs) != len(l.views) {
		return
	}

	for i := range l.views {
		v := &l.views[i]
		v.cursor, v.offset, v.filter.query = r.tabs[i].cursor, r.tabs[i].offset, r.tabs[i].query
		// A rebuild keeps the cursor only for a reader who moved it, and the
		// resize following every open rebuilds.
		v.touched = r.tabs[i].placed()
	}

	l.adopt()
	l.rebuild()
	l.place(r.tabs[l.at])
}

// place puts the cursor on the row a mark names, or at the index it held when
// that row has gone.
func (l *List) place(m tabMark) {
	if m.key != "" {
		for i := range l.lines {
			if row := l.lines[i].row; row != nil && row.Key == m.key {
				l.cursorTo(i, m.offset)

				return
			}
		}
	}

	l.cursorTo(m.cursor, m.offset)
}

func (l *List) cursorTo(cursor, offset int) {
	l.to(cursor)
	l.offset = min(offset, max(0, len(l.lines)-l.visible()))
}
