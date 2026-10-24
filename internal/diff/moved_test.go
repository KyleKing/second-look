package diff_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/diff"
)

const movedPatch = `diff --git a/app.py b/app.py
--- a/app.py
+++ b/app.py
@@ -10,7 +10,2 @@
 x = 1
-def helper(x):
-    total = x * 2
-    total += 1
-    return total
-
 y = 2
diff --git a/util.py b/util.py
--- a/util.py
+++ b/util.py
@@ -1,1 +1,8 @@
 import os
+class Util:
+    def helper(x):
+        total = x * 2
+        total += 1
+        return total
+
+    def other(y):
`

// A run removed from one file and added to another, at any indent, is a move
// with both ends named; the line around it that matches nothing is not.
func TestMovedNamesBothEndsOfARun(t *testing.T) {
	t.Parallel()

	moved := diff.Parse([]byte(movedPatch)).Moved()

	tests := []struct {
		name string
		ref  diff.LineRef
		want diff.Move
	}{
		{
			name: "the run's first added line",
			ref:  diff.LineRef{Path: "util.py", Kind: diff.KindAdd, New: 3},
			want: diff.Move{Path: "app.py", Line: 11, First: true, Lines: 4},
		},
		{
			name: "a removed line inside the run",
			ref:  diff.LineRef{Path: "app.py", Kind: diff.KindRemove, Old: 13},
			want: diff.Move{Path: "util.py", Line: 5, Lines: 4},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := moved[tc.ref]; got != tc.want {
				t.Errorf("moved = %+v, want %+v", got, tc.want)
			}
		})
	}

	if _, ok := moved[diff.LineRef{Path: "util.py", Kind: diff.KindAdd, New: 2}]; ok {
		t.Error("the new class line was read as moved")
	}
}

// A moved line pairs with nothing in its own block, so an unrelated line added
// beside it is not word-marked against it.
func TestMovedLinesStayOutOfWordPairing(t *testing.T) {
	t.Parallel()

	patch := strings.Replace(movedPatch, "-\n y = 2", "-\n+def helper(y):\n y = 2", 1)
	patch = strings.Replace(patch, "@@ -10,7 +10,2 @@", "@@ -10,7 +10,3 @@", 1)

	if got := diff.Parse([]byte(patch)).Refine(); len(got) != 0 {
		t.Errorf("a moved line was paired by position: %v", got)
	}
}

// The same lines taken out and put back in one block are an edit, not a move.
func TestMovedLeavesAnEditInPlaceAlone(t *testing.T) {
	t.Parallel()

	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,3 +1,3 @@\n" +
		"-\tfirst(a)\n-\tsecond(b)\n-\tthird(c)\n+\t\tfirst(a)\n+\t\tsecond(b)\n+\t\tthird(c)\n"

	if got := diff.Parse([]byte(patch)).Moved(); len(got) != 0 {
		t.Errorf("an edit in place was read as moved: %v", got)
	}
}
