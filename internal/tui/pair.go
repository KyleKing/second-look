package tui

import "github.com/kyleking/second-look/internal/diff"

// pairer turns a hunk's lines into code rows and whatever hangs off each.
//
// Unified that is one row per line, in patch order. Side by side, a block of
// removals is paired with the additions that replaced it so both sides of one
// edit sit on one row: the first removal answers the first addition, which is
// the same pairing the word-level marking uses, and an unanswered line leaves
// the other half of its row blank.
//
// A row anchors to its after side where it has one, so every comment, every
// motion, and the editor all address the line they always did and none of them
// has to know the screen is split.
type pairer struct {
	split bool
	// hang is the threads and comments sitting under one line, which follow the
	// row the line was drawn on rather than the line itself.
	hang func(diff.Line) []row
	path string
	hunk int
	// out is the caller's own rows, appended to in place: a slice of its own
	// per hunk would copy every row of a long file a second time.
	out *[]row

	gone []diff.Line
	came []diff.Line
}

// take adds one line, which may emit nothing until the block it belongs to
// closes.
func (p *pairer) take(l diff.Line) {
	if !p.split {
		p.emit(l, diff.Line{})

		return
	}

	switch l.Kind {
	case diff.KindRemove:
		// An addition already seen closes the block, so what follows opens a
		// new one rather than pairing across an unrelated boundary.
		if len(p.came) > 0 {
			p.flush()
		}

		p.gone = append(p.gone, l)
	case diff.KindAdd:
		p.came = append(p.came, l)
	default:
		p.flush()
		p.emit(l, diff.Line{})
	}
}

func (p *pairer) flush() {
	for at := range max(len(p.gone), len(p.came)) {
		switch {
		case at >= len(p.came):
			p.emit(p.gone[at], diff.Line{})
		case at >= len(p.gone):
			p.emit(p.came[at], diff.Line{})
		default:
			p.emit(p.came[at], p.gone[at])
		}
	}

	p.gone, p.came = nil, nil
}

// emit is one row and everything hanging off the lines it draws. The mate's
// conversations come first: it is the older side, and a comment on the line
// that came out reads before the answer to it.
func (p *pairer) emit(line, mate diff.Line) {
	*p.out = append(*p.out, row{
		kind: rowCode, line: line, mate: mate, path: p.path, comment: noComment, hunk: p.hunk,
	})

	if mate.Kind != 0 {
		*p.out = append(*p.out, p.hang(mate)...)
	}

	*p.out = append(*p.out, p.hang(line)...)
}

// sidesOf is the two lines a split row draws, left then right. A context line
// is both sides of itself, and a row answering only one side leaves the other
// blank.
func sidesOf(r row) (diff.Line, diff.Line) {
	switch r.line.Kind {
	case diff.KindContext:
		return r.line, r.line
	case diff.KindAdd:
		return r.mate, r.line
	default:
		return r.line, r.mate
	}
}
