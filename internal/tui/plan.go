package tui

import (
	"fmt"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/order"
)

// part is one run of a file's hunks drawn in one place. A file whose hunks were
// gathered into two groups is two parts, which is why the file heading has to
// say how much of itself is elsewhere.
type part struct {
	file  int
	path  string
	hunks map[int]bool
}

// planGroup is one heading and everything under it, whether the heading names a
// symbol the hunks answer for or the directory they share.
type planGroup struct {
	dir   string
	sym   bool
	made  bool
	files int
	hunks int
	parts []part
}

// named reports whether the heading says anything the file paths under it do
// not. One directory over a review whose files each carry their whole path is
// the same words twice; a gathered symbol, or what a machine wrote, is not.
func (g planGroup) named(groups int) bool { return groups > 1 || g.sym || g.made }

func (g planGroup) heading() string {
	what := fmt.Sprintf("%s · %s", plural(g.files, "file"), plural(g.hunks, "hunk"))

	switch {
	case g.made:
		return g.dir + "  " + what + " · counted, not read"
	case g.sym:
		return g.dir + "  " + what + " · gathered"
	}

	return g.dir + "  " + what
}

// groups is the review in the order it is read: the plan the structural pass
// made where there is one, and the diff's own directories where there is not.
//
// The fallback is what the screen shows for the second or two before the pass
// answers, on a review of a language nothing parses, and wherever the reader
// has asked for the diff's own order back.
func (l layout) groups(d *diff.Diff) []planGroup {
	if l.plan == nil {
		return byDirectory(d, l.made)
	}

	at := fileIndex(d)

	out := make([]planGroup, 0, len(l.plan))

	for _, g := range l.plan {
		group := planGroup{dir: g.Name, sym: g.Symbol, made: g.Made}

		for _, h := range g.Hunks {
			i, ok := at[h.Path]
			if !ok {
				continue
			}

			group.hunks++

			if n := len(group.parts); n > 0 && group.parts[n-1].path == h.Path {
				group.parts[n-1].hunks[h.Hunk] = true

				continue
			}

			group.files++
			group.parts = append(group.parts, part{
				file: i, path: h.Path, hunks: map[int]bool{h.Hunk: true},
			})
		}

		if len(group.parts) > 0 {
			out = append(out, group)
		}
	}

	return out
}

// byDirectory is the diff's own order: each file under the directory it sits
// in, kept in the order the diff named them, with what a machine wrote last.
func byDirectory(d *diff.Diff, made generated.Set) []planGroup {
	var (
		out  []planGroup
		last = planGroup{dir: order.Generated, made: true}
		at   = map[string]int{}
	)

	for i := range d.Files {
		path := filePath(&d.Files[i])
		whole := part{file: i, path: path}
		count := hunkCount(&d.Files[i])

		if made.Match(path) {
			last.parts = append(last.parts, whole)
			last.files++
			last.hunks += count

			continue
		}

		dir := dirOf(path)

		j, ok := at[dir]
		if !ok {
			j = len(out)
			at[dir] = j

			out = append(out, planGroup{dir: dir})
		}

		out[j].parts = append(out[j].parts, whole)
		out[j].files++
		out[j].hunks += count
	}

	if len(last.parts) == 0 {
		return out
	}

	return append(out, last)
}

// splitFiles is how many other places each file is also drawn in, so a heading
// can say it is showing part of one.
func splitFiles(groups []planGroup) map[string]int {
	seen := map[string]int{}

	for _, g := range groups {
		for _, p := range g.parts {
			seen[p.path]++
		}
	}

	for path, n := range seen {
		seen[path] = n - 1
	}

	return seen
}

// partWord says a file is showing only some of itself. A reader who does not
// know they are looking at half a file draws the wrong conclusion from it, and
// `]f` walks to the rest.
//
// The count is of the whole file rather than of this piece, because how much of
// a file is left is the question a piece of it cannot answer.
func (l layout) partWord(path string, elsewhere int) string {
	if elsewhere < 1 {
		return ""
	}

	read, total := l.progress(path)

	return fmt.Sprintf("  part of this file · %s elsewhere · %d/%d read · ]f walks to it",
		plural(elsewhere, "piece"), read, total)
}

func fileIndex(d *diff.Diff) map[string]int {
	out := make(map[string]int, len(d.Files))
	for i := range d.Files {
		out[filePath(&d.Files[i])] = i
	}

	return out
}

// cycleOrder puts the diff's own order back, and takes it away again.
//
// Gathering reads a symbol from syntax rather than resolving one, so it will
// sometimes put two unrelated hunks together or leave a pair apart. This is the
// way out, and it is also the way to see what the forge thought the order
// should be, which is a judgment worth being able to consult.
func (m *Model) cycleOrder() {
	if len(m.shape.plan) == 0 {
		m.say("nothing has been gathered: the structural pass has not answered", false)

		return
	}

	// The rows are laid out again from scratch, so an index means nothing across
	// the change: what the reader was looking at has to be found again.
	was := m.here()

	m.asDiffed = !m.asDiffed
	m.rebuild()
	m.goTo(was)

	if m.asDiffed {
		m.say("the diff's own order", false)

		return
	}

	m.say("gathered by symbol", false)
}

// orderWord names the order in the title when it is not the one that gathers,
// since a review read in the diff's order looks like one nothing was found in.
func (m *Model) orderWord() string {
	if m.asDiffed && len(m.shape.plan) > 0 {
		return "  as diffed"
	}

	return ""
}

// where names what the cursor is standing on in terms that survive a re-layout,
// with nth counting which occurrence of it, since two rows can read alike.
type where struct {
	comment int
	at      hunkAt
	kind    rowKind
	line    diff.Line
	thread  int
	note    int
	block   int
	text    string
	nth     int
}

func (m *Model) here() where {
	if m.cursor >= len(m.screen.rows) {
		return where{comment: noComment, nth: -1}
	}

	r := m.screen.rows[m.cursor]
	w := where{
		comment: r.comment, at: hunkAt{r.path, r.hunk}, kind: r.kind,
		line: r.line, thread: r.thread, note: r.note, block: r.block, text: r.text,
	}

	for i := range m.cursor {
		if same(m.screen.rows[i], w) {
			w.nth++
		}
	}

	return w
}

// same is whether a row is the row the cursor was on, read by what it is about
// rather than the marks a pass or a fold can set on it since.
func same(r row, w where) bool {
	return r.kind == w.kind && r.path == w.at.path && r.hunk == w.at.hunk &&
		r.line == w.line && r.comment == w.comment && r.thread == w.thread &&
		r.note == w.note && r.block == w.block && r.text == w.text
}

// find is where what the cursor was standing on went, answered as an index into
// the screen's rows: the row itself, else the first row of its hunk, else -1.
func (m *Model) find(was where) int {
	best := -1

	for i := range m.screen.rows {
		r := m.screen.rows[i]

		switch {
		case was.comment >= 0 && r.comment == was.comment:
			return i
		case same(r, was):
			return i
		case best < 0 && r.path == was.at.path && r.hunk == was.at.hunk:
			best = i
		}
	}

	return best
}

// goTo puts the cursor back on what it was standing on, and on the nearest
// thing to it where that row is gone: the hunk it was in, then the top.
func (m *Model) goTo(was where) {
	m.cursor = max(0, m.find(was))
	m.reveal()
}

// stay puts the cursor back on the row it was on across a re-layout, keeping
// its place on the frame; a row that is gone leaves the index as laid out.
func (m *Model) stay(was where) {
	if was.nth < 0 {
		return
	}

	n := 0

	for i := range m.screen.rows {
		if !same(m.screen.rows[i], was) {
			continue
		}

		if n == was.nth {
			m.offset += i - m.cursor
			m.cursor = i

			return
		}

		n++
	}
}
