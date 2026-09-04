package tui

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/order"
)

// The review's own body and note sit in the comment index space as sentinels.
// They are prose a person wrote and edits, so they have to be rows a cursor
// lands on, and they are not comments, so nothing acting on a comment finds one.
const (
	noComment  = -1
	reviewBody = -2
	reviewNote = -3
)

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

// shown reports whether a block is drawn in full. Everything is open until it
// is folded by hand: a note is the evidence for the comment above it, and one
// folded by default is one nobody reads.
func (f folds) shown(index int) bool {
	open, ok := f[index]

	return !ok || open
}

// folded is what z has put away by hand: a note, a run of removed lines, a
// hunk, or a whole file. Everything is open until it is in one of these maps,
// except a comment in the code view, which the marker folds by default.
type folded struct {
	notes folds
	// turns default the other way: an exchange is history and the comment as it
	// now reads is the thing being reviewed, so it is collapsed until asked for.
	turns map[int]bool
	hunks map[hunkAt]bool
	files map[string]bool
	gone  map[goneAt]bool
}

func newFolded() folded {
	return folded{
		notes: folds{}, turns: map[int]bool{}, hunks: map[hunkAt]bool{},
		files: map[string]bool{}, gone: map[goneAt]bool{},
	}
}

// layout is what laying out a review takes beyond the review itself: the width
// to wrap to, which hunks are hidden because nothing in them changed, and what
// has been folded away by hand.
type layout struct {
	width int
	hide  hider
	fold  folded
	// split pairs each removal with the addition that replaced it, so the two
	// sides of an edit share a row. It is the one renderer that changes which
	// rows exist rather than only how they are drawn.
	split bool
	// parsed is what the structural pass saw, said on each heading. It is nil
	// for every renderer but the structural one, and until the pass lands.
	parsed *shape
	// made is what counts as written by a machine, which is grouped last and
	// folded until somebody asks for it.
	made generated.Set
	// plan is the reading order the structural pass worked out, and nil where
	// there is none: before the pass answers, where nothing parsed, and where
	// the reader asked for the diff's own order back.
	plan []order.Group
}

// shut reports whether a file is drawn as one row rather than in full. What a
// machine wrote starts that way and z opens it, which inverts the default the
// rest of the review has: everything else is open until it is folded by hand.
func (l layout) shut(path string) bool {
	if l.made.Match(path) {
		return !l.fold.files[path]
	}

	return l.fold.files[path]
}

// hunkWord is what the parser saw of one hunk, and nothing where no pass has
// been asked for.
func (l layout) hunkWord(at hunkAt) string {
	if l.parsed == nil {
		return ""
	}

	return l.parsed.symbolWord(at)
}

// fileWord is what the parser saw of a whole file, drawn under its name so a
// reader can decide what to open before opening any of it.
func (l layout) fileWord(path string) string {
	if l.parsed == nil {
		return ""
	}

	return l.parsed.fileWord(path)
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

	// The two blocks are separated, because a note that opens where a body ended
	// reads as more of the body. The gap keeps the rail, so the review's own
	// prose still reads as one bounded block.
	rows := prose(reviewBody, "REVIEW BODY", r.Body, rowComment, avail, lay)
	rows = append(rows, row{kind: rowComment, comment: reviewNote})

	return append(rows, prose(reviewNote, "REVIEW NOTE", r.Note, rowNote, avail, lay)...)
}

// prose is one titled block of the review's own writing. The kind is what its
// lines are drawn as, so the note reads as local the way a comment's note does
// and the body reads as what will post.
func prose(index int, title, text string, kind rowKind, avail int, lay layout) []row {
	head := row{kind: rowComment, text: title, comment: index, head: true}

	if text == "" {
		head.text = title + "  empty · e to write one"

		return []row{head}
	}

	lines := wrap(text, avail)
	if !lay.fold.notes.shown(index) {
		head.text = fmt.Sprintf("%s  %s · za to read", title, plural(len(lines), "line"))
		head.folded = true

		return []row{head}
	}

	rows := make([]row, 0, len(lines)+1)
	rows = append(rows, head)

	for _, line := range lines {
		rows = append(rows, row{kind: kind, text: line, comment: index})
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

	rows = append(rows, turnRows(c, index, path, avail, lay)...)

	return append(rows, noteRows(c.Note, index, path, avail, lay)...)
}

// commentHead names the severity in caps because it is what the eye is looking
// for on a screen of code, and keeps the glyph so a monochrome terminal still
// says whether the comment will post.
func commentHead(c *artifact.Comment) string {
	head := fmt.Sprintf("%s %s  %s%s", statusGlyph(c.Status), strings.ToUpper(c.Severity), c.Status, span(c))

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
	if !lay.fold.notes.shown(index) {
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

// span is the lines a comment covers, and nothing for one on a single line. A
// comment renders under its end line, so a range that said nothing read as a
// comment on that line alone and left the lines above it looking untouched.
//
// The sides are named only where they differ, which GitHub allows and which
// means the range crosses a change rather than sitting on one side of it.
func span(c *artifact.Comment) string {
	if c.StartLine == 0 || c.StartLine == c.Line {
		return ""
	}

	if c.StartSide != "" && c.StartSide != c.Side {
		return fmt.Sprintf("  lines %s %d-%s %d", c.StartSide, c.StartLine, c.Side, c.Line)
	}

	return fmt.Sprintf("  lines %d-%d", c.StartLine, c.Line)
}

const turnLabel = "TURNS  "

// Collapsed, an exchange shows its last turn trimmed to lastLines and one line
// of the turn before it. Length is what makes a turn thread unreadable, so the
// collapsed shape is the design and za is the affordance.
const (
	lastLines   = 2
	beforeLines = 1
)

// turnRows is the exchange about a comment: what the author asked for and what
// the agent answered.
//
// It renders progressively because a comment three rounds deep is a page of
// prose sitting between the reader and the next hunk. The last thing said is
// what matters, the one before it says what it answers, and everything older is
// a count until it is asked for.
func turnRows(c *artifact.Comment, index int, path string, avail int, lay layout) []row {
	if len(c.Turns) == 0 {
		return nil
	}

	if lay.fold.turns[index] {
		rows := []row{{
			kind: rowTurn, path: path, comment: index,
			text: turnLabel + plural(len(c.Turns), "turn") + " · za to fold",
		}}

		for i := range c.Turns {
			rows = append(rows, turnLines(&c.Turns[i], index, path, avail, 0)...)
		}

		return rows
	}

	head := row{kind: rowTurn, path: path, comment: index, folded: true, text: turnLabel + turnHead(c)}
	rows := []row{head}

	at := len(c.Turns) - 1
	if at > 0 {
		rows = append(rows, turnLines(&c.Turns[at-1], index, path, avail, beforeLines)...)
	}

	return append(rows, turnLines(&c.Turns[at], index, path, avail, lastLines)...)
}

func turnHead(c *artifact.Comment) string {
	// The head counts what is not on screen: the last turn and the one before it
	// are drawn, so everything older is what the count is for.
	const drawn = 2

	if n := len(c.Turns) - drawn; n > 0 {
		return fmt.Sprintf("%s · %s earlier · za to read", plural(len(c.Turns), "turn"), plural(n, "turn"))
	}

	return plural(len(c.Turns), "turn") + " · za to read"
}

// turnLines is one turn under its author, trimmed to at most keep lines, where
// zero keeps all of them.
func turnLines(t *artifact.Turn, index int, path string, avail, keep int) []row {
	lines := wrap(t.Body, avail-bodyIndent)

	trimmed := false
	if keep > 0 && len(lines) > keep {
		lines, trimmed = lines[:keep], true
	}

	rows := make([]row, 0, len(lines)+1)
	rows = append(rows, row{kind: rowTurn, text: "@" + t.Author, path: path, comment: index})

	for i, l := range lines {
		text := strings.Repeat(" ", bodyIndent) + l
		if trimmed && i == len(lines)-1 {
			text += "…"
		}

		rows = append(rows, row{kind: rowTurn, text: text, path: path, comment: index})
	}

	return rows
}
