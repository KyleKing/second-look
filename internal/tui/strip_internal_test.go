package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"
)

// The header counts a diff once and keeps the answer, so a restage that hands
// the screen another diff has to be counted afresh rather than read off the
// old one's counts.
func TestStats_RecountsAReplacedDiff(t *testing.T) {
	t.Parallel()

	m := model(t, onePatch, nil)

	first := m.stats()
	if len(first) != 1 || first[0].added != 2 || first[0].removed != 1 {
		t.Fatalf("stats = %+v, want pkg/read.go +2 -1", first)
	}

	m.diff = diff.Parse([]byte("diff --git a/a/x.go b/a/x.go\n--- a/a/x.go\n+++ b/a/x.go\n@@ -1 +1,2 @@\n x\n+y\n" +
		"diff --git a/b/x.go b/b/x.go\n--- a/b/x.go\n+++ b/b/x.go\n@@ -1 +1 @@\n-x\n+z\n"))

	got := m.stats()
	if len(got) != 2 || got[0].path != "a/x.go" || got[1].changed() != 2 {
		t.Errorf("stats after a restage = %+v, want a/x.go and b/x.go", got)
	}

	if m.header.dirs["a"] != "a" || m.header.dirs["b"] != "b" {
		t.Errorf("directory names after a restage = %v, want a and b", m.header.dirs)
	}
}

// The strip's run is kept between frames, so moving the cursor inside a file
// reads the same place through it as walking the rows afresh would, and a
// rebuilt screen is walked again.
func TestThrough_KeptRunMatchesAFreshWalk(t *testing.T) {
	t.Parallel()

	m := model(t, alternating(3), nil)
	k := runKey{name: "pkg/f1.go"}

	fresh := func() float64 {
		m.stripRuns = runCaches{}

		return m.through(k)
	}

	for i := range m.screen.rows {
		if m.screen.rows[i].path != k.name {
			continue
		}

		m.cursor = i
		kept := m.through(k)

		if want := fresh(); kept != want {
			t.Fatalf("on row %d: kept run reads %v, a fresh walk %v", i, kept, want)
		}

		m.through(k)
	}

	m.cursor = len(m.screen.rows) / 2
	_ = m.through(k)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})

	if got, want := m.through(k), fresh(); got != want {
		t.Errorf("after a rebuild: %v, a fresh walk %v", got, want)
	}
}

// footerHeight answers without drawing where the footer is one line, and has
// to agree with what footerLines draws in every state.
func TestFooterHeight_MatchesFooterLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  func(*Model)
	}{
		{"hints", func(*Model) {}},
		{"a status", func(m *Model) { m.status = "saved" }},
		{"a long failure", func(m *Model) {
			m.status, m.failed = strings.Repeat("the post failed for a reason worth reading ", 8), true
		}},
		{"a short failure", func(m *Model) { m.status, m.failed = "no", true }},
		{"searching", func(m *Model) { m.searching = true }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := model(t, onePatch, nil)
			tt.set(m)

			if got, want := m.footerHeight(), len(m.footerLines()); got != want {
				t.Errorf("footerHeight = %d, footerLines draws %d", got, want)
			}
		})
	}
}
