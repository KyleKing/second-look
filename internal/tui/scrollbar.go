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
func (m *Model) overviewMarks() []overview.Mark {
	if m.view != viewDiff {
		return nil
	}

	out := make([]overview.Mark, 0, len(m.screen.rows)/4)

	for i := range m.screen.rows {
		if mk, ok := m.markFor(i, &m.screen.rows[i]); ok {
			out = append(out, mk)
		}
	}

	return out
}

// markFor is the mark row i puts on the track, if it puts one there.
func (m *Model) markFor(i int, r *row) (overview.Mark, bool) {
	p := m.styles.palette

	switch {
	case r.kind == rowCode && r.line.Kind == diff.KindAdd:
		return overview.Change(i, p.Green), true
	case r.kind == rowCode && r.line.Kind == diff.KindRemove:
		return overview.Change(i, p.Red), true
	case r.role != roleTop:
	case r.kind == rowThread && m.awaitsRuling(r.thread):
		return overview.Act(i, p.Peach), true
	case r.kind == rowComment && m.isDraft(r.comment):
		return overview.Draft(i, p.Yellow), true
	}

	if r.kind == rowHunk && r.hunk != 0 && m.read != nil && m.read.Has(m.hunkID(r.path, r.hunk)) {
		return overview.Done(i, p.Overlay1), true
	}

	return overview.Mark{}, false
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
