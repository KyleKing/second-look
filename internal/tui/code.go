package tui

import (
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
)

// viewMode is which of the three the screen is showing. They are the same rows
// in every case, so every motion, the search, and every action work in all
// three unchanged.
type viewMode int

const (
	// Both: the change and what is being said about it.
	viewDiff viewMode = iota
	// The code alone, as it reads after the change, with what is said about it
	// folded to a marker.
	viewCode
	// What will post, by file, with the diff left out.
	viewComments
)

// next cycles the views: both, then the code, then the comments.
func (v viewMode) next() viewMode {
	if v == viewComments {
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
	byLine, byThread := indexComments(r), indexThreads(ts)
	placed := make([]bool, len(r.Comments))

	s.rows = append(s.rows, header(r, lay, s.numWidth)...)

	for _, g := range group(d) {
		s.rows = append(s.rows, row{kind: rowBlank, comment: -1},
			row{kind: rowGroup, text: g.heading(), path: g.dir, comment: -1})

		for _, i := range g.files {
			s.rows = append(s.rows, s.codeRows(&d.Files[i], d, r, ts, byLine, byThread, placed, lay)...)
		}
	}

	return s.appendUnanchored(r, placed, lay)
}

func (s screen) codeRows(
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

	hunk, gone, folded, hide := 0, 0, 0, false

	for _, ln := range f.Lines {
		if ln.Hunk != hunk {
			rows = append(rows, dropped(gone, p, hunk)...)
			gone = 0
			hunk = ln.Hunk
			hide = lay.hide.skip != nil && lay.hide.skip(p, hunk)
			shut := lay.fold.hunks[hunkAt{p, hunk}]

			switch {
			case hide:
				folded++
			case shut:
				rows = append(rows, row{
					kind: rowHunk, text: codeHeader(d, hunk) + "  folded · za to open",
					path: p, comment: -1, hunk: hunk, folded: true,
				})
			default:
				rows = append(rows, row{
					kind: rowHunk, text: codeHeader(d, hunk), path: p, comment: -1, hunk: hunk,
				})
			}

			hide = hide || shut
		}

		if hide {
			claim(byLine, placed, p, ln)

			continue
		}

		// A removal has no line in the file that results, so a run of them
		// stands as one row saying how much came out.
		if ln.Kind == diff.KindRemove {
			gone++

			claim(byLine, placed, p, ln)

			continue
		}

		rows = append(rows, dropped(gone, p, hunk)...)
		gone = 0

		rows = append(rows, row{kind: rowCode, line: ln, path: p, comment: -1, hunk: hunk})

		for _, t := range byThread[anchorOf(p, ln)] {
			rows = append(rows, threadMarker(&ts[t], t, p)...)
		}

		for _, c := range byLine[anchorOf(p, ln)] {
			placed[c] = true
			rows = append(rows, commentMarker(&r.Comments[c], c, p, lay, s.numWidth)...)
		}
	}

	rows = append(rows, dropped(gone, p, hunk)...)

	if folded > 0 {
		rows = append(rows, row{
			kind: rowHunk, path: p, comment: -1,
			text: plural(folded, "hunk") + " hidden: " + lay.hide.why,
		})
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

// dropped is the run of removed lines as one row.
func dropped(n int, path string, hunk int) []row {
	if n == 0 {
		return nil
	}

	return []row{{
		kind: rowGone, path: path, comment: -1, hunk: hunk,
		text: plural(n, "line") + " removed",
	}}
}

// commentMarker is one comment as a single row, unless it has been opened.
func commentMarker(c *artifact.Comment, index int, path string, lay layout, numWidth int) []row {
	if lay.fold.notes[index] {
		return commentRows(c, index, path, lay, numWidth)
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
