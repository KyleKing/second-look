package tui

import (
	"path"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
)

const (
	stripFiles = 120
	labelShare = 3
)

type fileStat struct {
	path           string
	added, removed int
}

func (f fileStat) changed() int { return f.added + f.removed }

func (m *Model) stats() []fileStat {
	out := make([]fileStat, len(m.diff.Files))

	for i := range m.diff.Files {
		out[i].path = filePath(&m.diff.Files[i])

		for _, l := range m.diff.Files[i].Lines {
			switch l.Kind {
			case diff.KindAdd:
				out[i].added++
			case diff.KindRemove:
				out[i].removed++
			}
		}
	}

	return out
}

func paths(files []fileStat) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}

	return out
}

// label is the one place a file gets its name in the header, which chapter names will replace.
func label(file string, files []fileStat) string { return shortestTail(file, paths(files)) }

func (m *Model) currentLabel(files []fileStat) string {
	current := m.cursorPath()
	if current == "" {
		return ""
	}

	name := fitPath(label(current, files), max(1, m.width/labelShare))
	if n := m.screen.rows[min(m.cursor, len(m.screen.rows)-1)].line.New; n > 0 && name != "" {
		name += ":" + strconv.Itoa(n)
	}

	return name
}

type seg struct {
	weight    int
	read, lit bool
	// at is how far through the lit segment the cursor stands, from 0 to 1, where marked.
	at     float64
	marked bool
}

// inFile is the file the cursor is in and how far through it.
type inFile struct {
	path string
	at   float64
}

type cluster struct {
	label string
	segs  []seg
}

func (m *Model) readFiles() map[string]bool {
	done, total := map[string]int{}, map[string]int{}

	if m.read == nil {
		return nil
	}

	for _, ref := range seen.Hunks(m.diff) {
		total[ref.Path]++

		if m.read.Has(ref.ID) {
			done[ref.Path]++
		}
	}

	out := map[string]bool{}
	for p, n := range total {
		out[p] = done[p] == n
	}

	return out
}

func (m *Model) strip(files []fileStat, room int) string {
	read, current := m.readFiles(), m.cursorPath()
	dirs := directories(files)
	here := inFile{path: current, at: m.through(func(r row) bool { return r.path == current })}

	attempts := []struct {
		byFile, labels bool
	}{{true, true}, {false, true}, {false, false}}

	for _, a := range attempts {
		if a.byFile && len(files) > stripFiles {
			continue
		}

		cs := clusters(files, dirs, read, here, a.byFile)
		if widths, ok := stripWidths(cs, a.labels, room); ok {
			return m.draw(cs, widths, a.labels)
		}
	}

	cs := clusters(files, dirs, read, here, false)
	widths := make([]int, len(cs))

	for i := range widths {
		widths[i] = 1
	}

	return cut(m.draw(cs, widths, false), room)
}

func directories(files []fileStat) map[string]string {
	var all []string

	seenDir := map[string]bool{}

	for _, f := range files {
		if d := path.Dir(f.path); !seenDir[d] {
			seenDir[d] = true
			all = append(all, d)
		}
	}

	out := make(map[string]string, len(all))
	for _, d := range all {
		out[d] = shortestTail(d, all)
	}

	return out
}

func clusters(files []fileStat, names map[string]string, read map[string]bool, here inFile, byFile bool) []cluster {
	var (
		out   []cluster
		index = map[string]int{}
	)

	for _, f := range files {
		d := path.Dir(f.path)

		at, ok := index[d]
		if !ok {
			at = len(out)
			index[d] = at

			name := names[d]
			if d == "." {
				name = ""
			}

			out = append(out, cluster{label: name})
		}

		s := seg{weight: f.changed(), read: read[f.path], lit: f.path == here.path}
		s.marked, s.at = byFile && s.lit, here.at

		switch {
		case byFile || len(out[at].segs) == 0:
			out[at].segs = append(out[at].segs, s)
		default:
			g := &out[at].segs[0]
			g.weight += s.weight
			g.read = g.read && s.read
			g.lit = g.lit || s.lit
		}
	}

	return out
}

const (
	groupGap = 2
	fileGap  = "┊"
)

func stripWidths(cs []cluster, labels bool, room int) ([]int, bool) {
	fixed, n := groupGap*(len(cs)-1), 0
	var weights []int

	for _, c := range cs {
		if labels && c.label != "" {
			fixed += textWidth(c.label) + 1
		}

		fixed += len(c.segs) - 1
		n += len(c.segs)

		for _, s := range c.segs {
			weights = append(weights, s.weight)
		}
	}

	cells := room - fixed
	if cells < n {
		return nil, false
	}

	return share(weights, cells), true
}

func share(weights []int, cells int) []int {
	out := make([]int, len(weights))
	extra, total := cells-len(weights), 0

	for _, w := range weights {
		total += w
	}

	if total == 0 {
		for i := range out {
			out[i] = 1 + extra/len(out)
		}

		for i := range extra % len(out) {
			out[i]++
		}

		return out
	}

	rest := make([]int, len(weights))
	given := 0

	for i, w := range weights {
		out[i] = 1 + extra*w/total
		rest[i] = extra * w % total
		given += out[i] - 1
	}

	for ; given < extra; given++ {
		best := 0

		for i := range rest {
			if rest[i] > rest[best] {
				best = i
			}
		}

		out[best]++
		rest[best] = -1
	}

	return out
}

func (m *Model) draw(cs []cluster, widths []int, labels bool) string {
	var b strings.Builder

	at := 0

	for i, c := range cs {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", groupGap))
		}

		if labels && c.label != "" {
			b.WriteString(m.styles.hunk.Render(c.label) + " ")
		}

		for j, s := range c.segs {
			if j > 0 {
				b.WriteString(m.styles.behind.Render(fileGap))
			}

			b.WriteString(m.segment(s, widths[at]))

			at++
		}
	}

	return b.String()
}

// segment draws a segment, with a mark on the lit one where the cursor stands in it.
func (m *Model) segment(s seg, width int) string {
	line := m.face(s)
	if !s.marked || width < 2 {
		return line.Render(strings.Repeat(stripGlyph(s), width))
	}

	k := min(width-1, int(s.at*float64(width)))

	return line.Render(strings.Repeat(stripGlyph(s), k)) + m.styles.cursor.Render(stripMark) +
		line.Render(strings.Repeat(stripGlyph(s), width-k-1))
}

const stripMark = "●"

// through is how far the cursor stands through the run of rows around it that
// belong, from 0 at the run's first row to 1 at its last.
func (m *Model) through(belongs func(row) bool) float64 {
	rows := m.screen.rows
	if len(rows) == 0 {
		return 0
	}

	at := min(m.cursor, len(rows)-1)
	lo, hi := at, at

	for lo > 0 && belongs(rows[lo-1]) {
		lo--
	}

	for hi+1 < len(rows) && belongs(rows[hi+1]) {
		hi++
	}

	if hi == lo {
		return 0
	}

	return float64(at-lo) / float64(hi-lo)
}

func stripGlyph(s seg) string {
	if s.read {
		return "━"
	}

	return "─"
}

func (m *Model) face(s seg) lipgloss.Style {
	switch {
	case s.lit:
		return m.styles.selected.Bold(true)
	case s.read:
		return m.styles.behind
	}

	return m.styles.subtitle
}
