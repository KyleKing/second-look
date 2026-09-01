package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
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
	rowFile rowKind = iota
	rowHunk
	rowCode
	rowComment
	rowThread
	rowBlank
)

// row is one rendered line of the review screen. Comments occupy rows of their
// own so the cursor lands on prose the same way it lands on code.
type row struct {
	kind rowKind
	text string
	line diff.Line
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
func build(r *artifact.Review, d *diff.Diff, ts []threads.Thread, width int) screen {
	s := screen{numWidth: numberWidth(d)}
	byLine := indexComments(r)
	byThread := indexThreads(ts)
	placed := make([]bool, len(r.Comments))

	s.rows = append(s.rows, header(r, width-s.numWidth-rail)...)

	for i := range d.Files {
		f := &d.Files[i]
		path := filePath(f)
		s.rows = append(s.rows, row{kind: rowBlank, comment: -1},
			row{kind: rowFile, text: path, path: path, comment: -1})

		if f.Note != "" {
			s.rows = append(s.rows, row{kind: rowHunk, text: f.Note, path: path, comment: -1})
		}

		hunk := 0

		for _, l := range f.Lines {
			if l.Hunk != hunk {
				hunk = l.Hunk
				s.rows = append(s.rows, row{
					kind: rowHunk, text: hunkHeader(d, hunk), path: path, comment: -1, hunk: hunk,
				})
			}

			s.rows = append(s.rows, row{kind: rowCode, line: l, path: path, comment: -1, hunk: hunk})

			// What is already on GitHub comes before what this pass is adding,
			// so a comment reads as an answer to the conversation above it.
			for _, t := range byThread[anchorOf(path, l)] {
				s.rows = append(s.rows, threadRows(&ts[t], t, path, width, s.numWidth)...)
			}

			for _, c := range byLine[anchorOf(path, l)] {
				placed[c] = true
				s.rows = append(s.rows, comment(&r.Comments[c], c, path, width, s.numWidth)...)
			}
		}
	}

	return s.appendUnanchored(r, placed, width)
}

// appendUnanchored lists comments no diff line claimed. Staging refuses those,
// so reaching one means the diff moved under a review that was already staged.
func (s screen) appendUnanchored(r *artifact.Review, placed []bool, width int) screen {
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
		s.rows = append(s.rows, comment(c, i, c.Path, width, s.numWidth)...)
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

// header is the review's own prose, which has nowhere else to live: the title
// bar already names the pull request, so this carries only what a person wrote
// about the whole change.
func header(r *artifact.Review, width int) []row {
	var rows []row

	for _, block := range [][2]string{{"body", r.Body}, {"note", r.Note}} {
		if block[1] == "" {
			continue
		}

		rows = append(rows, row{kind: rowHunk, text: "review " + block[0], comment: -1})
		for _, l := range wrap(block[1], width) {
			rows = append(rows, row{kind: rowComment, text: l, comment: -1})
		}
	}

	return rows
}

func comment(c *artifact.Comment, index int, path string, width, numWidth int) []row {
	avail := width - numWidth - rail
	head := fmt.Sprintf("%s %s", statusGlyph(c.Status), c.Severity)

	if c.InReplyTo != 0 {
		head += fmt.Sprintf(" reply to %d", c.InReplyTo)
	}

	if c.Status == artifact.StatusSkip && c.SkipReason != "" {
		head += " — " + c.SkipReason
	}

	body, note := wrap(c.Body, avail), wrap(c.Note, avail-bodyIndent)
	rows := make([]row, 0, 1+len(body)+len(note))
	rows = append(rows, row{kind: rowComment, text: head, path: path, comment: index, head: true})

	for _, l := range body {
		rows = append(rows, row{kind: rowComment, text: l, path: path, comment: index})
	}

	for _, l := range note {
		rows = append(rows, row{kind: rowComment, text: "· " + l, path: path, comment: index})
	}

	return rows
}

// buildList is the review without the diff: every comment that will post,
// under the file it belongs to, with the counts on the heading.
//
// It is the same rows the diff view uses, so every motion, the search, and
// every action work in it unchanged. Skipped comments are counted rather than
// listed: a finding considered and declined is worth recording and not worth
// re-reading, and the diff view still shows it where it sits.
func buildList(r *artifact.Review, d *diff.Diff, width int) screen {
	s := screen{numWidth: numberWidth(d)}
	s.rows = append(s.rows, header(r, width-s.numWidth-rail)...)

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

			s.rows = append(s.rows, comment(&r.Comments[i], i, path, width, s.numWidth)...)
		}
	}

	if len(s.rows) == len(header(r, width-s.numWidth-rail)) {
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
	avail := width - numWidth - rail - bodyIndent
	rows := []row{{
		kind: rowThread, text: fmt.Sprintf("⤷ open thread · %d comment(s)", len(t.Notes)),
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
func wrap(text string, width int) []string {
	if text == "" {
		return nil
	}

	if width < 1 {
		width = 1
	}

	var out []string

	for _, para := range strings.Split(text, "\n") {
		line := ""

		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case textWidth(line)+1+textWidth(word) <= width:
				line += " " + word

				continue
			default:
				out = append(out, line)
				line = word
			}

			if textWidth(line) > width {
				chunks := split(line, width)
				out = append(out, chunks[:len(chunks)-1]...)
				line = chunks[len(chunks)-1]
			}
		}

		out = append(out, line)
	}

	return out
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
