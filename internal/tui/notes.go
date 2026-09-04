package tui

import (
	"strings"

	"github.com/kyleking/second-look/internal/ghmd"
	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/threads"
)

// What a comment from GitHub is allowed to spend before it starts folding. Every
// one of them is a budget rather than a limit: what they hold back is a za away.
const (
	// A fence longer than this is drawn to its head and folded after.
	fenceFold = 10
	// How much of a folded fence is still drawn.
	fenceHead = 6
	// How many rows one comment gets before the rest waits for za.
	noteCap = 20
	// How wide a thematic break is drawn, short of the measure the prose is set
	// to: one running the full width reads as the end of the comment rather
	// than as a break inside it.
	ruleWidth = 24
	// How far a held-back row is inset past the block it stands in for.
	holdIndent = bodyIndent * 2
)

// noteAt names one comment inside one open thread.
type noteAt struct {
	thread int
	note   int
}

// blockAt names one foldable thing inside one comment, numbered in the order
// the comment is walked so a block nested in a <details> gets a key of its own.
// The tail the height cap holds back is tailBlock.
type blockAt struct {
	noteAt
	block int
}

// tailBlock is what the rest of an over-long comment folds under. It is
// negative so it can never collide with a block's own number, and blockOffset
// lifts every key a row carries past it, which leaves zero meaning "this row
// folds nothing".
const (
	tailBlock   = -1
	blockOffset = 2
)

// noted parses every open thread's comments. A rebuild happens on every
// keystroke and the bodies do not change, so this is done once.
func noted(ts []threads.Thread) map[noteAt][]ghmd.Block {
	out := map[noteAt][]ghmd.Block{}

	for i := range ts {
		for j := range ts[i].Notes {
			out[noteAt{i, j}] = ghmd.Parse(ts[i].Notes[j].Body)
		}
	}

	return out
}

// threadRows renders one conversation already on GitHub: who said what, in
// order, under the line it hangs from. It is read-only, and answering it stages
// a comment in the prepared review like any other.
func threadRows(t *threads.Thread, index int, path string, numWidth int, lay layout, where string) []row {
	s := scribe{
		at: index, path: path, lay: lay,
		// bodyIndent is applied after wrapping, so it comes off the width first.
		avail: proseCols(lay.width, numWidth) - bodyIndent,
	}

	rows := make([]row, 0, len(t.Notes)*2+1)
	rows = append(rows, row{
		kind: rowThread, text: "⤷ " + lead(where) + " · " + plural(len(t.Notes), "comment"),
		path: path, comment: -1, thread: index, head: true,
	})

	for i := range t.Notes {
		rows = append(rows, row{
			kind: rowThread, text: "@" + t.Notes[i].Author, path: path, comment: -1, thread: index,
		})
		rows = append(rows, s.note(i)...)
	}

	return rows
}

// scribe draws one thread's comments. It carries what every row of them needs,
// so the block readers take an index rather than six arguments each.
type scribe struct {
	at    int
	path  string
	avail int
	lay   layout
	// on is which comment is being drawn and next is the number the block being
	// drawn will fold under, which climbs through nested sections so no two
	// blocks of one comment share a key.
	on   int
	next int
}

// note is one comment: every block of it, then whatever the height cap held
// back. The cap counts drawn rows rather than blocks, because one fence can be
// longer than a whole comment of prose.
//
// It starts the block count again, because a fold is keyed by the comment it is
// in and the numbers only have to be unique inside one.
func (s *scribe) note(i int) []row {
	s.on, s.next = i, 0

	blocks := s.lay.notes[noteAt{s.at, i}]
	out := make([]row, 0, len(blocks))

	for at := range blocks {
		out = append(out, s.block(&blocks[at])...)
	}

	if len(out) <= noteCap || s.opened(tailBlock) {
		return out
	}

	return append(out[:noteCap:noteCap],
		s.holding(tailBlock, plural(len(out)-noteCap, "more line")))
}

func (s *scribe) block(b *ghmd.Block) []row {
	switch b.Kind {
	case ghmd.Code:
		return s.fence(b)
	case ghmd.Details:
		return s.section(b)
	case ghmd.Rule:
		return []row{s.row(strings.Repeat("─", min(s.avail, ruleWidth)))}
	case ghmd.Table:
		return s.flat(b.Lines, "")
	case ghmd.Quote:
		return s.flat(b.Lines, "│ ")
	case ghmd.Prose:
	}

	return s.wrapped(b.Lines)
}

// fence is code somebody pasted. Its lines are never wrapped, since reflowing
// code says something the code does not, and a long one is drawn to its head
// with the rest one keystroke away.
func (s *scribe) fence(b *ghmd.Block) []row {
	if len(b.Lines) <= fenceFold {
		return s.code(b, len(b.Lines))
	}

	at, word := s.claim(), fenceWord(b.Lang, len(b.Lines))
	if s.opened(at) {
		return append([]row{s.heading(at, true, word)}, s.code(b, len(b.Lines))...)
	}

	out := []row{s.heading(at, false, word)}
	out = append(out, s.code(b, fenceHead)...)

	return append(out, s.holding(at, plural(len(b.Lines)-fenceHead, "more line")))
}

// code draws the lines of a fence under whatever grammar its tag named, and as
// plain text where the tag named none.
func (s *scribe) code(b *ghmd.Block, upto int) []row {
	lit := highlight.Tagged(b.Lang, b.Lines)
	out := make([]row, 0, upto)

	for i, line := range b.Lines[:upto] {
		r := s.row(strings.Repeat(" ", bodyIndent) + expand(line))
		if i < len(lit) {
			r.lit = placed(lit[i], line, bodyIndent)
		}

		out = append(out, r)
	}

	return out
}

// section is a <details>, which GitHub draws collapsed and so does this. Its
// blocks are numbered whether or not it is open, so opening one does not
// renumber the folds of everything after it.
func (s *scribe) section(b *ghmd.Block) []row {
	at := s.claim()
	open := s.opened(at)
	out := []row{s.heading(at, open, sectionWord(b))}

	for i := range b.Blocks {
		inner := s.block(&b.Blocks[i])
		if open {
			out = append(out, inner...)
		}
	}

	return out
}

// wrapped is prose set to the comment's measure.
func (s *scribe) wrapped(lines []string) []row {
	set := wrap(strings.Join(lines, "\n"), s.avail)
	out := make([]row, 0, len(set))

	for _, l := range set {
		out = append(out, s.row(strings.Repeat(" ", bodyIndent)+l))
	}

	return out
}

// flat is lines drawn as they were written, cut at the frame rather than
// wrapped. A table row and a quoted line both lose what they are when reflowed.
func (s *scribe) flat(lines []string, lead string) []row {
	out := make([]row, 0, len(lines))

	for _, l := range lines {
		out = append(out, s.row(strings.Repeat(" ", bodyIndent)+lead+expand(l)))
	}

	return out
}

// claim takes the next fold number in this comment.
func (s *scribe) claim() int {
	s.next++

	return s.next - 1
}

func (s *scribe) opened(block int) bool {
	return s.lay.fold.blocks[blockAt{noteAt{s.at, s.on}, block}]
}

func (s *scribe) row(text string) row {
	return row{kind: rowThread, text: text, path: s.path, comment: -1, thread: s.at, note: s.on}
}

// heading is the row a foldable block hangs from, marked so za finds it and
// pointing the way the block is about to go.
func (s *scribe) heading(block int, open bool, text string) row {
	r := s.row(strings.Repeat(" ", bodyIndent) + arrow(open) + " " + text)
	r.block = block + blockOffset
	r.folded = !open

	return r
}

// holding is the row standing in for what was held back, which za opens the
// same way the heading does.
func (s *scribe) holding(block int, what string) row {
	r := s.row(strings.Repeat(" ", holdIndent) + "… " + what + " · za to read")
	r.block = block + blockOffset
	r.folded = true

	return r
}

// placed shifts a line's spans onto the row it is drawn on, past the indent the
// row carries and across the spaces each tab became. A span indexes the line
// the fence held, and the row is not that line.
func placed(spans []highlight.Span, line string, lead int) []highlight.Span {
	if len(spans) == 0 {
		return nil
	}

	where := make([]int, len(line)+1)
	col := lead

	for i := range line {
		where[i] = col

		if line[i] != '\t' {
			col++

			continue
		}

		col += tabStop - (col-lead)%tabStop
	}

	where[len(line)] = col

	out := make([]highlight.Span, 0, len(spans))
	for _, s := range spans {
		from, to := min(s.From, len(line)), min(s.To, len(line))
		out = append(out, highlight.Span{From: where[from], To: where[to], Class: s.Class})
	}

	return out
}

// foldables is how many blocks of a comment carry a fold, counting the ones
// inside a section the same way the scribe numbers them. Opening everything
// needs the count, because it opens what nobody has looked at yet.
func foldables(blocks []ghmd.Block) int {
	n := 0

	for i := range blocks {
		switch {
		case blocks[i].Kind == ghmd.Code && len(blocks[i].Lines) > fenceFold:
			n++
		case blocks[i].Kind == ghmd.Details:
			n += 1 + foldables(blocks[i].Blocks)
		}
	}

	return n
}

// foldKey is what a row folds under, and false for a row that folds nothing.
func foldKey(r row) (blockAt, bool) {
	if r.block == 0 {
		return blockAt{}, false
	}

	return blockAt{noteAt{r.thread, r.note}, r.block - blockOffset}, true
}

func arrow(open bool) string {
	if open {
		return "▾"
	}

	return "▸"
}

func fenceWord(lang string, lines int) string {
	if lang == "" {
		return plural(lines, "line") + " of code"
	}

	return lang + " · " + plural(lines, "line")
}

func sectionWord(b *ghmd.Block) string {
	if b.Summary != "" {
		return b.Summary
	}

	return plural(len(b.Blocks), "hidden block")
}

// lead names what a thread head says it is. The diff view draws a thread under
// the line it answers, so there it says only that it is open.
func lead(where string) string {
	if where == "" {
		return "open thread"
	}

	return where
}
