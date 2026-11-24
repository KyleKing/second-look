package tui

import (
	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/tui/overview"

	"github.com/kyleking/second-look/internal/artifact"
)

// scrollbar is the column on the right edge saying how much of the content the
// frame is showing and where in it, and what waits off screen. Content that
// fits gets none: a full-height thumb says the same thing as no bar at all and
// costs a column to say it.
func scrollbar(height, total, offset int, s styles) *overview.Bar {
	return overview.New(height, total, offset, s.subtitle, s.rail)
}

// alongside puts the bar on the right edge of lines already rendered, padding
// each to the same column so the bar is straight whatever the lines carry.
func alongside(lines []string, bar *overview.Bar, width int) []string {
	if bar == nil {
		return lines
	}

	for i := range lines {
		lines[i] = pad(lines[i], width-1) + bar.Cell(i)
	}

	return lines
}

// bodyWidth is what a row is cut to so it never collides with the bar. Content
// that fits keeps the column: a bar that says nothing is not worth a column of
// every row.
func bodyWidth(width int, bar *overview.Bar) int {
	return width - 1 - bar.Width()*trackWidth
}

// overviewMarks types the rows the track should place, so the bar is a map of
// the review: which way each change leans, the findings still waiting on a
// ruling, the drafts not yet sent, and the hunks already read. A row inside the
// frame is left to the thumb.
//
// Only one mark survives in each of the track's cells, so the rows are reduced
// to each cell's winner before any mark is made: the first row of the highest
// rank, which is what Place itself would keep. Every changed line is a
// candidate, so making a styled mark per row cost megabytes a frame on a long
// diff for a track a few dozen cells tall.
func (m *Model) overviewMarks(bar *overview.Bar) []overview.Mark {
	cells, total := bar.Height(), len(m.screen.rows)
	if m.view != viewDiff || cells == 0 || total == 0 {
		return nil
	}

	type winner struct{ row, rank int }

	best := make([]winner, cells)

	for i := range m.screen.rows {
		rank := m.rankOf(&m.screen.rows[i])
		if rank == 0 {
			continue
		}

		c := &best[i*cells/total]
		if c.rank < rank {
			*c = winner{row: i, rank: rank}
		}
	}

	out := make([]overview.Mark, 0, cells)

	for _, w := range best {
		if w.rank != 0 {
			out = append(out, m.markFor(w.row, &m.screen.rows[w.row]))
		}
	}

	return out
}

// rankOf is the rank of the mark a row puts on the track, or 0 for none.
func (m *Model) rankOf(r *row) int {
	switch {
	case r.kind == rowCode && (r.line.Kind == diff.KindAdd || r.line.Kind == diff.KindRemove):
		return overview.RankChange
	case r.role != roleTop:
	case r.kind == rowThread && m.awaitsRuling(r.thread):
		return overview.RankAct
	case r.kind == rowComment && m.isDraft(r.comment):
		return overview.RankDraft
	}

	if r.kind == rowHunk && r.hunk != 0 && m.read != nil && m.read.Has(m.hunkID(r.path, r.hunk)) {
		return overview.RankDone
	}

	return 0
}

// markFor is the mark row i puts on the track, which rankOf has said it puts.
func (m *Model) markFor(i int, r *row) overview.Mark {
	p := m.styles.palette

	switch m.rankOf(r) {
	case overview.RankChange:
		if r.line.Kind == diff.KindAdd {
			return overview.Change(i, p.Green)
		}

		return overview.Change(i, p.Red)
	case overview.RankAct:
		return overview.Act(i, p.Peach)
	case overview.RankDraft:
		return overview.Draft(i, p.Yellow)
	default:
		return overview.Done(i, p.Overlay1)
	}
}

// awaitsRuling reports a thread that is a finding nobody has ruled on.
func (m *Model) awaitsRuling(t int) bool {
	return t >= 0 && t < len(m.threads) && m.isFinding(&m.threads[t]) && m.verdictOfThread(t) == vNone
}

// isDraft reports a staged comment still marked draft.
func (m *Model) isDraft(c int) bool {
	return c >= 0 && c < len(m.review.Comments) && m.review.Comments[c].Status == artifact.StatusDraft
}

// trackWidth is the column the bar occupies where one is drawn.
const trackWidth = 1
