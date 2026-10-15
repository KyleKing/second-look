package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

const twoLangPatch = `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1,2 +1,3 @@
 package x
+func go1() int { return 1 }
diff --git a/y.ts b/y.ts
--- a/y.ts
+++ b/y.ts
@@ -1,2 +1,3 @@
 export {}
+function ts1(): number { return 1 }
diff --git a/deep/nested/z.go b/deep/nested/z.go
--- a/deep/nested/z.go
+++ b/deep/nested/z.go
@@ -1,2 +1,3 @@
 package z
+func go2() int { return 2 }
`

// f narrows the diff to the files a pattern keeps, live as it is typed. A
// file it hides keeps its heading and says why, the way a folded hunk does,
// so nothing ever looks missing without a reason.
func TestFileFilterKeepsOnlyMatchingFiles(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, twoLangPatch)

	pressKey(m, 'f')
	typeInto(m, "*.go")

	if !strings.Contains(plain(m.Frame()), "showing 2 of 3 files") {
		t.Fatalf("the prompt does not count what the pattern keeps:\n%s", plain(m.Frame()))
	}

	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	frame := plain(m.Frame())
	for _, want := range []string{"go1() int", "go2() int", "files: *.go"} {
		if !strings.Contains(frame, want) {
			t.Errorf("want %q in the frame:\n%s", want, frame)
		}
	}

	if strings.Contains(frame, "ts1(): number") {
		t.Errorf("the typeScript file is still drawn:\n%s", frame)
	}

	if !strings.Contains(frame, "1 hunk hidden: filtered by *.go") {
		t.Errorf("nothing says the file was filtered out:\n%s", frame)
	}
}

// A negated token drops its matches instead, and escape abandons an edit
// without losing the pattern that was committed.
func TestFileFilterNegatesAndEscapeKeepsTheCommittedPattern(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, twoLangPatch)

	pressKey(m, 'f')
	typeInto(m, "!*.go")
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "ts1(): number") {
		t.Errorf("the kept file is not drawn:\n%s", frame)
	}
	if strings.Contains(frame, "go1() int") {
		t.Errorf("a dropped file is still drawn:\n%s", frame)
	}

	// The prompt reopens with what was committed; typing past it and escaping
	// puts it back rather than clearing to nothing.
	pressKey(m, 'f')
	typeInto(m, " z")
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if !strings.Contains(plain(m.Frame()), "files: !*.go") {
		t.Errorf("escape lost the committed pattern:\n%s", plain(m.Frame()))
	}
}
