package tui

import (
	"testing"

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
