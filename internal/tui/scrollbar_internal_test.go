package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
)

// alternating is files of 30 lines cycling context, removed, and added, so
// every cell of a short track has several rows competing for it.
func alternating(files int) string {
	var patch strings.Builder

	for f := range files {
		path := fmt.Sprintf("pkg/f%d.go", f)
		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1,30 +1,30 @@\n", path, path, path, path)

		for l := range 30 {
			sign := [...]string{" context", "-removed", "+added"}[l%3]
			fmt.Fprintf(&patch, "%s %d\n", sign, l)
		}
	}

	return patch.String()
}

// overviewMarks hands Place one mark per cell rather than one per row, and the
// track has to come out the same as if every row's mark had been placed.
func TestOverviewMarks_SameTrackAsEveryRow(t *testing.T) {
	t.Parallel()

	m := model(t, alternating(6), func(r *artifact.Review) {
		r.Comments = []artifact.Comment{{
			ID: "d", Path: "pkg/f3.go", Line: 5, Side: artifact.SideRight,
			Body: "draft", Status: artifact.StatusDraft,
		}}
	})
	total := len(m.screen.rows)

	tests := []struct{ height, offset int }{
		{6, 0}, {13, 0}, {30, 0}, {13, 40}, {30, total - 30},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d rows at %d", tt.height, tt.offset), func(t *testing.T) {
			t.Parallel()

			every := scrollbar(tt.height, total, tt.offset, m.styles)

			for i := range m.screen.rows {
				if m.rankOf(&m.screen.rows[i]) != 0 {
					every.Place(m.markFor(i, &m.screen.rows[i]))
				}
			}

			reduced := scrollbar(tt.height, total, tt.offset, m.styles)
			marks := m.overviewMarks(reduced)
			reduced.Place(marks...)

			if len(marks) == 0 || len(marks) > tt.height {
				t.Errorf("%d marks for a %d-cell track", len(marks), tt.height)
			}

			if got, want := reduced.Lines(), every.Lines(); !slices.Equal(got, want) {
				t.Errorf("track\n%q\nwant\n%q", got, want)
			}
		})
	}
}
