package tui

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
)

// The review's own body and note sit in the comment index space as sentinels.
// They are prose a person wrote and edits, so they have to be rows a cursor
// lands on, and they are not comments, so nothing acting on a comment finds one.
const (
	noComment  = -1
	reviewBody = -2
	reviewNote = -3
)

// noteFold is how many wrapped lines a note shows before it starts folded. A
// note carries the evidence for a comment, which runs longer than the comment
// far more often than not, so unfolded it buries the prose it exists to support.
const noteFold = 2

// noteLabel introduces a note and sets the column its continuation lines align
// to, so a wrapped note reads as one block rather than as a list. It is capital
// because it is the block's label and not the first words of it.
const noteLabel = "NOTE  "

// How wide prose is set. A comment measured to the full width of a wide
// terminal is hard to read back, and the margin keeps it off the right edge.
const (
	proseWidth = 88
	rightPad   = 2
)

// proseCols is the measure a comment, a note, or the review's own prose is
// wrapped to.
func proseCols(width, numWidth int) int {
	return min(width-numWidth-rail-rightPad, proseWidth)
}

// folds records which notes are open against that default.
type folds map[int]bool

// shown reports whether a block of lines is drawn in full. A foldAt of zero
// never folds on its own and can still be closed by hand.
func (f folds) shown(index, lines, foldAt int) bool {
	if open, ok := f[index]; ok {
		return open
	}

	return foldAt == 0 || lines <= foldAt
}

// folded is what z has put away by hand: a note, a hunk, or a whole file. A
// note carries a default that depends on how long it is, so the map records the
// answer either way; a hunk or a file is open until somebody folds it.
type folded struct {
	notes folds
	hunks map[hunkAt]bool
	files map[string]bool
}

func newFolded() folded {
	return folded{notes: folds{}, hunks: map[hunkAt]bool{}, files: map[string]bool{}}
}

// layout is what laying out a review takes beyond the review itself: the width
// to wrap to, which hunks are hidden because nothing in them changed, and what
// has been folded away by hand.
type layout struct {
	width int
	hide  hider
	fold  folded
}

// header is the review's own prose. Both blocks are drawn whether or not
// anything is written in them: a review posted with no body is unsigned, and a
// field that appears only once it is filled in is one nobody knows to fill in.
func header(r *artifact.Review, lay layout, numWidth int) []row {
	if r.Body == "" && r.Note == "" {
		return []row{{
			kind: rowComment, comment: reviewBody, head: true,
			text: "REVIEW  no body, no note · e to write one",
		}}
	}

	avail := proseCols(lay.width, numWidth)

	return append(
		prose(reviewBody, "REVIEW BODY", r.Body, avail, 0, lay),
		prose(reviewNote, "REVIEW NOTE", r.Note, avail, noteFold, lay)...,
	)
}

// prose is one titled block of the review's own writing.
func prose(index int, title, text string, avail, foldAt int, lay layout) []row {
	head := row{kind: rowComment, text: title, comment: index, head: true}

	if text == "" {
		head.text = title + "  empty · e to write one"

		return []row{head}
	}

	lines := wrap(text, avail)
	if !lay.fold.notes.shown(index, len(lines), foldAt) {
		head.text = fmt.Sprintf("%s  %s · za to read", title, plural(len(lines), "line"))
		head.folded = true

		return []row{head}
	}

	rows := make([]row, 0, len(lines)+1)
	rows = append(rows, head)

	for _, line := range lines {
		rows = append(rows, row{kind: rowComment, text: line, comment: index})
	}

	return rows
}

// commentRows is one prepared comment: a heading naming what it is and whether
// it will post, the body at the contrast of the code it is about, and the local
// note under it.
func commentRows(c *artifact.Comment, index int, path string, lay layout, numWidth int) []row {
	avail := proseCols(lay.width, numWidth)
	body := wrap(c.Body, avail)

	rows := make([]row, 0, len(body)+3)
	rows = append(rows,
		row{kind: rowBlank, path: path, comment: index},
		row{kind: rowComment, text: commentHead(c), path: path, comment: index, head: true})

	for _, line := range body {
		rows = append(rows, row{kind: rowComment, text: line, path: path, comment: index})
	}

	return append(rows, noteRows(c.Note, index, path, avail, lay)...)
}

// commentHead names the severity in caps because it is what the eye is looking
// for on a screen of code, and keeps the glyph so a monochrome terminal still
// says whether the comment will post.
func commentHead(c *artifact.Comment) string {
	head := fmt.Sprintf("%s %s  %s", statusGlyph(c.Status), strings.ToUpper(c.Severity), c.Status)

	if c.InReplyTo != 0 {
		head += fmt.Sprintf("  reply to %d", c.InReplyTo)
	}

	if c.Status == artifact.StatusSkip && c.SkipReason != "" {
		head += "  " + c.SkipReason
	}

	return head
}

func noteRows(note string, index int, path string, avail int, lay layout) []row {
	if note == "" {
		return nil
	}

	lines := wrap(note, avail-len(noteLabel))
	if !lay.fold.notes.shown(index, len(lines), noteFold) {
		return []row{{
			kind: rowNote, path: path, comment: index, folded: true,
			text: noteLabel + plural(len(lines), "line") + " · za to read",
		}}
	}

	rows := make([]row, 0, len(lines))

	for i, line := range lines {
		text := strings.Repeat(" ", len(noteLabel)) + line
		if i == 0 {
			text = noteLabel + line
		}

		rows = append(rows, row{kind: rowNote, text: text, path: path, comment: index})
	}

	return rows
}
