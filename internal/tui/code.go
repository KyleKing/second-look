package tui

import (
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
)

// viewMode is which of the four the screen is showing. They are the same rows
// in every case, so every motion, the search, and every action work in all
// four unchanged.
//
// Only the first three are a cycle. The conversations are what the forge
// already holds rather than what this pass is staging, which is a different
// axis from how the change itself is drawn, so t reaches them and c leaves
// them alone.
type viewMode int

const (
	// Both: the change and what is being said about it.
	viewDiff viewMode = iota
	// The code alone, as it reads after the change, with what is said about it
	// folded to a marker.
	viewCode
	// What will post, by file, with the diff left out.
	viewComments
	// Every open conversation on the pull request, each under the line it
	// anchors to, with the rest of the diff left out.
	viewThreads
)

// next cycles the views: both, the code, then the comments. The conversations
// are off the cycle, so leaving them goes back to the diff.
func (v viewMode) next() viewMode {
	if v == viewComments || v == viewThreads {
		return viewDiff
	}

	return v + 1
}

func (v viewMode) String() string {
	switch v {
	case viewCode:
		return "code"
	case viewComments:
		return "comments"
	case viewThreads:
		return "threads"
	case viewDiff:
	}

	return ""
}

// buildCode is the file as it reads after the change: every line that survives
// it, with a removal standing as one marker and each conversation standing as
// one row naming what it is.
//
// It is for the reading a diff makes hard. A +/- pair says what changed and
// leaves working out what the code now says to the reader, which is the question
// a review actually turns on, and four comments on one hunk bury the code they
// are about. Folding both to a marker keeps them findable without them being in
// the way, and za opens the one under the cursor.
func buildCode(r *artifact.Review, d *diff.Diff, ts []threads.Thread, lay layout) screen {
	s := screen{numWidth: numberWidth(d)}
	placed := make([]bool, len(r.Comments))
	ctx := codeCtx{
		d: d, r: r, ts: ts, byLine: indexComments(r), byThread: indexThreads(ts),
		placed: placed, lay: lay,
	}

	s.rows = append(s.rows, header(r, lay, s.numWidth)...)

	for _, g := range lay.groups(d) {
		s.rows = append(s.rows, row{kind: rowBlank, comment: -1},
			row{kind: rowGroup, text: g.heading(), path: g.dir, comment: -1})

		for _, at := range g.parts {
			s.rows = append(s.rows, s.codeRows(&d.Files[at.file], at.hunks, ctx)...)
		}
	}

	return s.appendUnanchored(r, placed, lay)
}

// codeCtx is what laying out the code view needs beyond the file in front of
// it. The two indexes and the placed slice are shared across every file, so
// they travel as one value rather than as six arguments repeated at each call.
type codeCtx struct {
	d        *diff.Diff
	r        *artifact.Review
	ts       []threads.Thread
	byLine   map[anchor][]int
	byThread map[anchor][]int
	placed   []bool
	lay      layout
}

func (s screen) codeRows(f *diff.File, want map[int]bool, c codeCtx) []row {
	p := filePath(f)
	rows := []row{{kind: rowFile, text: p, path: p, comment: -1}}

	if c.lay.shut(p) {
		rows[0].text = p + "  " + plural(hunkCount(f), "hunk") + " folded" + staged(c.r, p) + " · za to open"
		rows[0].folded = true

		for _, ln := range f.Lines {
			claim(c.byLine, c.placed, p, ln)
		}

		return rows
	}

	hunk, folded, hide := 0, 0, false

	var run []diff.Line

	for _, ln := range f.Lines {
		if want != nil && !want[ln.Hunk] {
			continue
		}

		if ln.Hunk != hunk {
			rows = append(rows, s.removed(run, p, hunk, c)...)
			run = nil
			hunk = ln.Hunk
			hide = c.lay.hide.skip != nil && c.lay.hide.skip(p, hunk)
			shut := c.lay.fold.hunks[hunkAt{p, hunk}]

			switch {
			case hide:
				folded++
			case shut:
				rows = append(rows, row{
					kind: rowHunk, text: codeHeader(c.d, hunk) + "  folded · za to open",
					path: p, comment: -1, hunk: hunk, folded: true,
				})
			default:
				rows = append(rows, row{
					kind: rowHunk, text: codeHeader(c.d, hunk), path: p, comment: -1, hunk: hunk,
				})
			}

			hide = hide || shut
		}

		if hide {
			claim(c.byLine, c.placed, p, ln)

			continue
		}

		// A removal has no line in the file that results, so a run of them
		// stands where it came out rather than in the order it was written.
		if ln.Kind == diff.KindRemove {
			run = append(run, ln)

			continue
		}

		rows = append(rows, s.removed(run, p, hunk, c)...)
		run = nil

		rows = append(rows, row{kind: rowCode, line: ln, path: p, comment: -1, hunk: hunk})
		rows = append(rows, s.hanging(p, ln, c)...)
	}

	rows = append(rows, s.removed(run, p, hunk, c)...)

	if folded > 0 {
		rows = append(rows, row{
			kind: rowHunk, path: p, comment: -1,
			text: plural(folded, "hunk") + " hidden: " + c.lay.hide.why,
		})
	}

	return rows
}

// removed is a run of lines that came out: one row saying how many, and the
// lines themselves once za has opened it. Knowing something was there is
// enough to keep reading and not enough to review the change, so the two sides
// meet here rather than in a split, which is what every other fold on this
// screen already does.
//
// The run is named by the line it started on in the file it came from, since
// the file that results carries none of them.
func (s screen) removed(run []diff.Line, p string, hunk int, c codeCtx) []row {
	if len(run) == 0 {
		return nil
	}

	at := goneAt{path: p, old: run[0].Old}
	head := row{
		kind: rowGone, path: p, comment: -1, hunk: hunk, gone: at.old,
		text: plural(len(run), "line") + " removed",
	}

	if !c.lay.fold.gone[at] {
		for _, ln := range run {
			claim(c.byLine, c.placed, p, ln)
		}

		head.text += " · za to read"
		head.folded = true

		return []row{head}
	}

	rows := make([]row, 0, len(run)+1)
	rows = append(rows, head)

	for _, ln := range run {
		rows = append(rows, row{kind: rowCode, line: ln, path: p, comment: -1, hunk: hunk, gone: at.old})
		rows = append(rows, s.hanging(p, ln, c)...)
	}

	return rows
}

// goneRuns names every run of removed lines in the diff. A run folds itself, so
// zR has to be told what there is to open.
func goneRuns(d *diff.Diff) []goneAt {
	var out []goneAt

	for i := range d.Files {
		p := filePath(&d.Files[i])
		last := 0

		for _, ln := range d.Files[i].Lines {
			if ln.Kind != diff.KindRemove {
				last = 0

				continue
			}

			if last == 0 {
				out = append(out, goneAt{path: p, old: ln.Old})
			}

			last = ln.Old
		}
	}

	return out
}

// hanging is what sits under one line of code: the open conversations on it,
// then the comments this review is staging against it.
func (s screen) hanging(p string, ln diff.Line, c codeCtx) []row {
	a := anchorOf(p, ln)
	rows := make([]row, 0, len(c.byThread[a])+len(c.byLine[a]))

	for _, t := range c.byThread[a] {
		rows = append(rows, threadMarker(&c.ts[t], t, p)...)
	}

	for _, i := range c.byLine[a] {
		c.placed[i] = true
		rows = append(rows, commentMarker(&c.r.Comments[i], i, p, c.lay, s.numWidth)...)
	}

	return rows
}

// codeHeader says where in the file that resulted the lines below it start, and
// whatever the forge named as the region they sit in. A @@ pair counts lines on
// both sides of a change, which is vocabulary the code view has no use for.
func codeHeader(d *diff.Diff, hunk int) string {
	raw := hunkHeader(d, hunk)

	span, tail, ok := strings.Cut(strings.TrimPrefix(raw, "@@ "), " @@")
	if !ok {
		return raw
	}

	at := ""

	for _, field := range strings.Fields(span) {
		if after, found := strings.CutPrefix(field, "+"); found {
			at, _, _ = strings.Cut(after, ",")
		}
	}

	if at == "" {
		return raw
	}

	return "line " + at + "  " + strings.TrimSpace(tail)
}

// commentMarker is one comment as a single row, unless it has been opened.
func commentMarker(c *artifact.Comment, index int, path string, lay layout, numWidth int) []row {
	if lay.fold.notes[index] {
		return commentRows(c, index, path, lay, numWidth, inRun{})
	}

	return []row{{
		kind: rowComment, path: path, comment: index, head: true, folded: true,
		text: "▸ " + commentHead(c) + "  " + firstLine(c.Body, proseCols(lay.width, numWidth)),
	}}
}

func threadMarker(t *threads.Thread, index int, path string) []row {
	return []row{{
		kind: rowThread, path: path, comment: -1, thread: index, head: true,
		text: "▸ ⤷ open thread · " + plural(len(t.Notes), "comment") + " · @" + t.Notes[0].Author,
	}}
}

// firstLine is as much of a body as one row has for it.
func firstLine(body string, width int) string {
	lines := wrap(body, width)
	if len(lines) == 0 {
		return ""
	}

	if len(lines) == 1 {
		return lines[0]
	}

	return lines[0] + "…"
}
