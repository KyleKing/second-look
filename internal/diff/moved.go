package diff

import "strings"

// Move is where the other end of a moved line sits: the line it was taken from,
// for one the change adds, or the line it went to, for one it removes.
type Move struct {
	Path string
	Line int
	// First marks the run's opening line, and Lines is how long the run is.
	First bool
	Lines int
}

// moveFloor is the fewest lines a run needs before it reads as moved, since two
// matching lines are as often a coincidence as a move.
const moveFloor = 3

type spot struct{ file, line int }

// Moved is both ends of every run removed in one hunk and added in another,
// matched without indent. The same text taken out and put back in one hunk is an edit.
func (d *Diff) Moved() map[LineRef]Move {
	removed := map[string][]spot{}

	for f := range d.Files {
		for i, l := range d.Files[f].Lines {
			if l.Kind == KindRemove {
				k := moveKey(l.Text)
				removed[k] = append(removed[k], spot{f, i})
			}
		}
	}

	out := map[LineRef]Move{}
	used := map[spot]bool{}

	for f := range d.Files {
		lines := d.Files[f].Lines

		for i := 0; i < len(lines); {
			n, from := d.longestRun(spot{f, i}, removed[moveKey(lines[i].Text)], used)
			if n == 0 {
				i++

				continue
			}

			d.record(out, spot{f, i}, from, n)

			for k := range n {
				used[spot{from.file, from.line + k}] = true
			}

			i += n
		}
	}

	return out
}

// moveStartCap bounds how many removed runs one added line checks against.
// Past it the candidates are duplicates of the same few lines — generated
// code and blank-ish lines — where checking more finds no better move but
// costs quadratically in the duplicates.
const moveStartCap = 32

// longestRun is the longest run starting at an added line that some unused
// removal run matches line for line, and where that removal starts.
func (d *Diff) longestRun(at spot, starts []spot, used map[spot]bool) (int, spot) {
	came := d.Files[at.file].Lines
	if came[at.line].Kind != KindAdd || !meaningful(came[at.line].Text) {
		return 0, spot{}
	}

	best, from := 0, spot{}

	for _, s := range starts[:min(len(starts), moveStartCap)] {
		gone := d.Files[s.file].Lines
		if s.file == at.file && gone[s.line].Hunk == came[at.line].Hunk {
			continue
		}

		n, meant := matching(came[at.line:], gone[s.line:], func(k int) bool { return used[spot{s.file, s.line + k}] })
		if meant >= moveFloor && n > best {
			best, from = n, s
		}
	}

	return best, from
}

// matching is how many leading lines of came the unused lines of gone repeat,
// trailing blanks dropped, and how many of those are meaningful.
func matching(came, gone []Line, taken func(int) bool) (int, int) {
	n, meant := 0, 0

	for n < len(came) && n < len(gone) && !taken(n) {
		c, g := came[n], gone[n]
		if c.Kind != KindAdd || g.Kind != KindRemove || moveKey(c.Text) != moveKey(g.Text) {
			break
		}

		if meaningful(c.Text) {
			meant++
		}

		n++
	}

	for n > 0 && moveKey(came[n-1].Text) == "" {
		n--
	}

	return n, meant
}

func (d *Diff) record(out map[LineRef]Move, at, from spot, n int) {
	came, gone := &d.Files[at.file], &d.Files[from.file]

	for k := range n {
		c, g := came.Lines[at.line+k], gone.Lines[from.line+k]
		out[refOf(pathOf(came), c)] = Move{Path: pathOf(gone), Line: g.Old, First: k == 0, Lines: n}
		out[refOf(pathOf(gone), g)] = Move{Path: pathOf(came), Line: c.New, First: k == 0, Lines: n}
	}
}

func moveKey(text string) string { return strings.TrimSpace(text) }

// meaningful reports whether a line carries enough to match on, so a run does
// not open on a blank or a lone brace.
func meaningful(text string) bool { return len(moveKey(text)) >= indentFloor }
