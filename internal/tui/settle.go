package tui

import "github.com/kyleking/aragonite/tui/glyph"

// Text from GitHub (a title, a comment, a reaction, a snippet) is settled
// before it is measured: a VS16 heart, a ZWJ sequence, or a text-default emoji
// is a width the terminal and lipgloss can disagree on, and a row that holds
// one has its borders and right column moved. glyph.Settle rewrites each into
// something every terminal counts the same, and leaves wide emoji and CJK,
// which they already agree on.

// settled settles every span a queue row draws, in place: rows are built fresh
// for each rebuild, and settling is idempotent.
func settled(secs []Section) []Section {
	for i := range secs {
		for j := range secs[i].Rows {
			settleRow(&secs[i].Rows[j])
		}
	}

	return secs
}

func settleRow(r *Row) {
	for key, c := range r.Cells {
		settleSpans(c.Spans)
		settleSpans(c.Long)
		r.Cells[key] = c
	}

	if r.About != nil {
		r.About.Subject = glyph.Settle(r.About.Subject)
		for i := range r.About.Facts {
			r.About.Facts[i].Text = glyph.Settle(r.About.Facts[i].Text)
		}
	}
}

func settleSpans(spans []Span) {
	for i := range spans {
		spans[i].Text = glyph.Settle(spans[i].Text)
	}
}
