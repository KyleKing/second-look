package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/threads"
)

// Gutter widths: indent is the two columns a header block is inset by, and
// rail is what the numbers plus the comment rail take from a comment's width.
const (
	indent = 2
	rail   = 4
	// A continuation line is inset by bodyIndent under the name or the marker
	// that introduces it.
	bodyIndent = 2
)

type rowKind int

const (
	rowGroup rowKind = iota
	rowFile
	rowHunk
	rowCode
	rowComment
	rowNote
	rowGone
	rowThread
	rowBlank
)

// row is one rendered line of the review screen. Comments occupy rows of their
// own so the cursor lands on prose the same way it lands on code.
type row struct {
	kind rowKind
	text string
	line diff.Line
	// mate is the other side of the same edit, drawn beside line where the
	// renderer is side by side and ignored everywhere else. A row answering only
	// one side leaves it zero.
	mate diff.Line
	path string
	// comment indexes Review.Comments for every row of a comment block, so an
	// action taken anywhere inside the block finds it.
	comment int
	// thread indexes the open threads for every row of a thread block. It is
	// only read where kind is rowThread, so its zero elsewhere means nothing.
	thread int
	// hunk numbers the @@ block a row belongs to, across the whole diff, and is
	// zero for a row that belongs to none.
	hunk int
	// head marks the first row of a comment block, which is where a jump lands.
	head bool
	// folded marks a row standing in for lines it is hiding, which is what za
	// inverts.
	folded bool
	// gone marks a row belonging to a run of removed lines, numbered by the
	// line the run started on. Zero belongs to no run.
	gone int
}

// screen is the flattened review: the diff with each open thread and each
// prepared comment inserted under the line it anchors to.
type screen struct {
	rows     []row
	numWidth int
}

// build flattens the diff and the prepared review into rows at the given width.
// A comment whose path is absent from the diff is listed at the end rather than
// dropped, because a comment nobody can see is a comment nobody can retract.
func build(r *artifact.Review, d *diff.Diff, ts []threads.Thread, lay layout) screen {
	s := screen{numWidth: numberWidth(d)}
	byLine := indexComments(r)
	byThread := indexThreads(ts)
	placed := make([]bool, len(r.Comments))

	s.rows = append(s.rows, header(r, lay, s.numWidth)...)

	for _, g := range group(d) {
		s.rows = append(s.rows, row{kind: rowBlank, comment: -1},
			row{kind: rowGroup, text: g.heading(), path: g.dir, comment: -1})

		for _, i := range g.files {
			s.rows = append(s.rows, s.fileRows(&d.Files[i], d, r, ts, byLine, byThread, placed, lay)...)
		}
	}

	return s.appendUnanchored(r, placed, lay)
}

// fileRows is one file: its name, whatever it says about itself, and every hunk
// with the threads and comments that hang off each line.
func (s screen) fileRows(
	f *diff.File, d *diff.Diff, r *artifact.Review, ts []threads.Thread,
	byLine, byThread map[anchor][]int, placed []bool, lay layout,
) []row {
	p := filePath(f)
	rows := []row{{kind: rowFile, text: p, path: p, comment: -1}}

	if lay.fold.files[p] {
		rows[0].text = p + "  " + plural(hunkCount(f), "hunk") + " folded" + staged(r, p) + " · za to open"
		rows[0].folded = true

		for _, ln := range f.Lines {
			claim(byLine, placed, p, ln)
		}

		return rows
	}

	if f.Note != "" {
		rows = append(rows, row{kind: rowHunk, text: f.Note, path: p, comment: -1})
	}

	if word := lay.fileWord(p); word != "" {
		rows = append(rows, row{kind: rowHunk, text: word, path: p, comment: -1})
	}

	hunk, folded, hide := 0, 0, false
	lines := &pairer{
		split: lay.split, path: p, out: &rows,
		hang: s.hanger(p, r, ts, byLine, byThread, placed, lay),
	}

	for _, l := range f.Lines {
		if l.Hunk != hunk {
			// A change block never spans two hunks, so the pending one closes
			// before the next heading is drawn.
			lines.flush()
			lines.hunk = l.Hunk
			hunk = l.Hunk

			// The tests walk the file, so they are asked once per hunk rather
			// than once per line.
			head, skipped := hunkRow(d, lay, hunkAt{p, hunk})
			if head != nil {
				rows = append(rows, *head)
			}

			hide = skipped
			if hide && head == nil {
				folded++
			}
		}

		if hide {
			claim(byLine, placed, p, l)

			continue
		}

		lines.take(l)
	}

	lines.flush()

	if folded > 0 {
		rows = append(rows, row{
			kind: rowHunk, path: p, comment: -1,
			text: plural(folded, "hunk") + " hidden: " + lay.hide.why,
		})
	}

	return rows
}

// hanger answers with the threads and comments sitting on one line, which
// follow the row that line was drawn on: side by side, that row carries two
// lines and both hang off it.
func (s screen) hanger(
	p string, r *artifact.Review, ts []threads.Thread,
	byLine, byThread map[anchor][]int, placed []bool, lay layout,
) func(diff.Line) []row {
	return func(l diff.Line) []row {
		a := anchorOf(p, l)
		out := make([]row, 0, len(byThread[a])+len(byLine[a]))

		// What is already on GitHub comes before what this pass is adding, so a
		// comment reads as an answer to the conversation above it.
		for _, t := range byThread[a] {
			out = append(out, threadRows(&ts[t], t, p, lay.width, s.numWidth)...)
		}

		for _, c := range byLine[a] {
			placed[c] = true
			out = append(out, commentRows(&r.Comments[c], c, p, lay, s.numWidth)...)
		}

		return out
	}
}

// hunkRow is one hunk's own row and whether its lines are held back. A hunk the
// current fold level hides has no row at all and is counted instead; one folded
// by hand keeps its row and says so.
func hunkRow(d *diff.Diff, lay layout, at hunkAt) (*row, bool) {
	if lay.hide.skip != nil && lay.hide.skip(at.path, at.hunk) {
		return nil, true
	}

	if lay.fold.hunks[at] {
		return &row{
			kind: rowHunk, text: hunkHeader(d, at.hunk) + "  folded · za to open",
			path: at.path, comment: noComment, hunk: at.hunk, folded: true,
		}, true
	}

	return &row{
		kind: rowHunk, comment: noComment, path: at.path, hunk: at.hunk,
		text: hunkHeader(d, at.hunk) + lay.hunkWord(at),
	}, false
}

// claim marks the comments anchored to a line without drawing them, so a hunk
// folded away does not send its comments to the list of what the diff no longer
// carries, which is where a review staged against a head that moved goes.
func claim(byLine map[anchor][]int, placed []bool, p string, l diff.Line) {
	for _, c := range byLine[anchorOf(p, l)] {
		placed[c] = true
	}
}

// staged is what a folded file is holding, since a fold that hid work would
// make the outline the one view that cannot be trusted.
func staged(r *artifact.Review, path string) string {
	c := countFor(r, path)
	if n := c.ready + c.draft; n > 0 {
		return " · " + plural(n, "comment")
	}

	return ""
}

// appendUnanchored lists comments no diff line claimed. Staging refuses those,
// so reaching one means the diff moved under a review that was already staged.
func (s screen) appendUnanchored(r *artifact.Review, placed []bool, lay layout) screen {
	var loose []int

	for i := range r.Comments {
		if !placed[i] {
			loose = append(loose, i)
		}
	}

	if len(loose) == 0 {
		return s
	}

	s.rows = append(s.rows, row{kind: rowBlank, comment: -1},
		row{kind: rowFile, text: fmt.Sprintf("not in this diff (%d)", len(loose)), comment: -1})

	for _, i := range loose {
		c := &r.Comments[i]
		s.rows = append(s.rows, row{
			kind: rowHunk, text: fmt.Sprintf("%s %s %d", c.Path, c.Side, c.Line), comment: -1,
		})
		s.rows = append(s.rows, commentRows(c, i, c.Path, lay, s.numWidth)...)
	}

	return s
}

// anchor identifies the diff line a comment points at. A context line carries
// both a pre-image and a post-image number, so it answers to a comment on
// either side.
type anchor struct {
	path string
	side string
	line int
}

func anchorOf(path string, l diff.Line) anchor {
	if l.Kind == diff.KindRemove {
		return anchor{path: path, side: artifact.SideLeft, line: l.Old}
	}

	return anchor{path: path, side: artifact.SideRight, line: l.New}
}

func indexComments(r *artifact.Review) map[anchor][]int {
	out := make(map[anchor][]int, len(r.Comments))

	for i := range r.Comments {
		c := &r.Comments[i]
		a := anchor{path: c.Path, side: c.Side, line: c.Line}
		out[a] = append(out[a], i)
	}

	return out
}

// buildList is the review without the diff: every comment that will post,
// under the file it belongs to, with the counts on the heading.
//
// It is the same rows the diff view uses, so every motion, the search, and
// every action work in it unchanged. Skipped comments are counted rather than
// listed: a finding considered and declined is worth recording and not worth
// re-reading, and the diff view still shows it where it sits.
func buildList(r *artifact.Review, d *diff.Diff, lay layout) screen {
	s := screen{numWidth: numberWidth(d)}
	s.rows = append(s.rows, header(r, lay, s.numWidth)...)

	for _, path := range commentPaths(r) {
		c := countFor(r, path)
		s.rows = append(s.rows, row{kind: rowBlank, comment: -1}, row{
			kind: rowFile, path: path, comment: -1,
			text: fmt.Sprintf("%s  %d ready · %d draft · %d skipped", path, c.ready, c.draft, c.skip),
		})

		for i := range r.Comments {
			if r.Comments[i].Path != path || r.Comments[i].Status == artifact.StatusSkip {
				continue
			}

			s.rows = append(s.rows, commentRows(&r.Comments[i], i, path, lay, s.numWidth)...)
		}
	}

	if len(s.rows) == len(header(r, lay, s.numWidth)) {
		s.rows = append(s.rows, row{kind: rowBlank, comment: -1},
			row{kind: rowFile, text: "no comments staged", comment: -1})
	}

	return s
}

// commentPaths is every file a comment sits on, in the order the diff carries
// them, so the list reads in the same order as the diff behind it.
func commentPaths(r *artifact.Review) []string {
	seenPath := map[string]bool{}

	var out []string

	for i := range r.Comments {
		if p := r.Comments[i].Path; p != "" && !seenPath[p] {
			seenPath[p] = true

			out = append(out, p)
		}
	}

	return out
}

func countFor(r *artifact.Review, path string) tally {
	var out tally

	for i := range r.Comments {
		if r.Comments[i].Path != path {
			continue
		}

		switch r.Comments[i].Status {
		case artifact.StatusReady:
			out.ready++
		case artifact.StatusDraft:
			out.draft++
		case artifact.StatusSkip:
			out.skip++
		}
	}

	return out
}

// indexThreads groups the open threads by the diff line they anchor to, the
// same way staged comments are grouped, so both render under the same line.
func indexThreads(ts []threads.Thread) map[anchor][]int {
	out := make(map[anchor][]int, len(ts))

	for i := range ts {
		t := &ts[i]
		out[anchor{path: t.Path, side: t.Side, line: t.Line}] = append(
			out[anchor{path: t.Path, side: t.Side, line: t.Line}], i,
		)
	}

	return out
}

// threadRows renders one conversation already on GitHub: who said what, in
// order, under the line it hangs from. It is read-only, and answering it stages
// a comment in the prepared review like any other.
func threadRows(t *threads.Thread, index int, path string, width, numWidth int) []row {
	// bodyIndent is applied after wrapping, so it comes off the width first.
	avail := proseCols(width, numWidth) - bodyIndent
	rows := []row{{
		kind: rowThread, text: "⤷ open thread · " + plural(len(t.Notes), "comment"),
		path: path, comment: -1, thread: index, head: true,
	}}

	for i := range t.Notes {
		n := &t.Notes[i]
		rows = append(rows, row{
			kind: rowThread, text: "@" + n.Author, path: path, comment: -1, thread: index,
		})

		for _, l := range wrap(n.Body, avail) {
			rows = append(rows, row{
				kind: rowThread, text: "  " + l, path: path, comment: -1, thread: index,
			})
		}
	}

	return rows
}

// dirGroup is the files of one directory, which in a Go tree is one package
// and in any tree is the unit people actually review together.
type dirGroup struct {
	dir   string
	files []int
	hunks int
}

func (g dirGroup) heading() string {
	return fmt.Sprintf("%s  %s · %s", g.dir, plural(len(g.files), "file"), plural(g.hunks, "hunk"))
}

// group collects the diff's files by the directory they sit in, keeping each
// directory in the order it first appears.
//
// Reading a change is reading one package at a time, and a flat list of paths
// makes the reader do that grouping in their head on every scroll. Sorting
// would be a different decision: the diff's own order carries the forge's
// judgment about what to show first, and this keeps it while making the
// boundaries visible.
func group(d *diff.Diff) []dirGroup {
	var (
		out   []dirGroup
		index = map[string]int{}
	)

	for i := range d.Files {
		dir := dirOf(filePath(&d.Files[i]))

		at, ok := index[dir]
		if !ok {
			at = len(out)
			index[dir] = at
			out = append(out, dirGroup{dir: dir})
		}

		out[at].files = append(out[at].files, i)
		out[at].hunks += hunkCount(&d.Files[i])
	}

	return out
}

// dirOf is the directory part of a diff path. A diff always spells paths with
// forward slashes whatever the platform, so this does not go through path/filepath.
func dirOf(p string) string {
	if at := strings.LastIndexByte(p, '/'); at > 0 {
		return p[:at]
	}

	return "."
}

func hunkCount(f *diff.File) int {
	last, n := 0, 0

	for _, l := range f.Lines {
		if l.Hunk != last {
			last = l.Hunk
			n++
		}
	}

	return n
}

func filePath(f *diff.File) string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}

func hunkHeader(d *diff.Diff, hunk int) string {
	if hunk >= 1 && hunk <= len(d.Headers) {
		return d.Headers[hunk-1]
	}

	return "@@"
}

// numberWidth sizes the line-number gutter to the widest number in the diff, so
// the code column does not shift between files.
func numberWidth(d *diff.Diff) int {
	const narrowest = 3

	widest := 0

	for i := range d.Files {
		for _, l := range d.Files[i].Lines {
			widest = max(widest, l.Old, l.New)
		}
	}

	return max(narrowest, len(strconv.Itoa(widest)))
}

// wrap breaks text at word boundaries in terminal cells, keeping any line
// breaks the author wrote. A word wider than the frame is broken rather than
// left to be truncated: a sentence in a script that puts no spaces between its
// words is one word, and dropping all but the first line of it loses the
// comment.
//
// A line's own indent is kept, and a line that opens a list item is wrapped
// under its text rather than under its marker, so where one item ends and the
// next begins survives the wrap. An agent writes in lists and a review read
// back as one paragraph is a review nobody finds the second finding in.
func wrap(text string, width int) []string {
	if text == "" {
		return nil
	}

	if width < 1 {
		width = 1
	}

	var out []string
	for _, para := range strings.Split(text, "\n") {
		out = append(out, wrapLine(para, width)...)
	}

	return out
}

func wrapLine(para string, width int) []string {
	body := strings.TrimLeft(para, " \t")
	lead := strings.Repeat(" ", textWidth(strings.ReplaceAll(para[:len(para)-len(body)], "\t", "    ")))
	pad := lead + strings.Repeat(" ", marker(body))
	avail := max(1, width-textWidth(pad))

	var (
		out  []string
		line string
	)

	for _, word := range strings.Fields(body) {
		switch {
		case line == "":
			line = word
		case textWidth(line)+1+textWidth(word) <= avail:
			line += " " + word

			continue
		default:
			out = append(out, line)
			line = word
		}

		if textWidth(line) > avail {
			chunks := split(line, avail)
			out = append(out, chunks[:len(chunks)-1]...)
			line = chunks[len(chunks)-1]
		}
	}

	out = append(out, line)

	for i := range out {
		if i == 0 {
			out[i] = lead + out[i]

			continue
		}

		out[i] = pad + out[i]
	}

	return out
}

// bulletWidth is the marker and the space after it, which is what a bullet or a
// single-digit number costs.
const bulletWidth = 2

// marker is how wide the list marker opening a line is, and zero for a line
// that opens no list. The bullets are the ones an agent writes in markdown.
func marker(body string) int {
	if len(body) > 1 && strings.ContainsRune("-*+", rune(body[0])) && body[1] == ' ' {
		return bulletWidth
	}

	digits := 0
	for digits < len(body) && body[digits] >= '0' && body[digits] <= '9' {
		digits++
	}

	if digits == 0 || digits+1 >= len(body) ||
		(body[digits] != '.' && body[digits] != ')') || body[digits+1] != ' ' {
		return 0
	}

	return digits + bulletWidth
}

// split breaks one over-wide word into frame-sized pieces, measured in cells so
// a double-width glyph is never left straddling the edge.
func split(word string, width int) []string {
	var (
		out  []string
		line string
	)

	for _, r := range word {
		if textWidth(line)+textWidth(string(r)) > width {
			out = append(out, line)
			line = ""
		}

		line += string(r)
	}

	return append(out, line)
}

// plural is humanize's, under a short name because this package counts things
// on nearly every row it draws.
func plural(n int, what string) string { return humanize.Plural(n, what) }
