package diag_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
)

const patch = `diff --git a/a.ts b/a.ts
--- a/a.ts
+++ b/a.ts
@@ -1,3 +1,4 @@
 const a = 1
-const b = 2
+const b = "two"
+const c = 3
 const d = 4
diff --git a/gone.ts b/gone.ts
--- a/gone.ts
+++ b/gone.ts
@@ -1,2 +1,2 @@
 const e = 5
 const f = 6
`

// The split is the whole of the filtering, so it is what a test has to pin: a
// note on a line the change wrote is a finding, one on a line it only carried
// is context, and one on a file it never touched is neither.
func TestPlaceSortsNotesAgainstTheChange(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(patch))

	got := diag.Place(d, []diag.Note{
		{Path: "a.ts", Line: 2, Message: "on a changed line", Severity: diag.Error},
		{Path: "a.ts", Line: 4, Message: "on a line it only carried"},
		{Path: "elsewhere.ts", Line: 1, Message: "on a file it never touched"},
		{Path: "gone.ts", Line: 1, Message: "on an untouched line of a touched file"},
	})

	if n := got.Count(); n != 1 {
		t.Errorf("counted %d notes on changed lines, want 1", n)
	}

	on := got.On[diag.Anchor{Path: "a.ts", Line: 2}]
	if len(on) != 1 || on[0].Message != "on a changed line" {
		t.Errorf("a.ts:2 carries %v, want the note on the changed line", on)
	}

	if len(got.Elsewhere["a.ts"]) != 1 || len(got.Elsewhere["gone.ts"]) != 1 {
		t.Errorf("context notes placed as %v", got.Elsewhere)
	}

	if _, ok := got.Elsewhere["elsewhere.ts"]; ok {
		t.Error("a note on a file the diff does not carry was kept")
	}
}

// A line carrying several notes lists the strongest first, so the reader who
// reads one reads the one that matters.
func TestPlaceOrdersALineByStrength(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(patch))

	at := diag.Anchor{Path: "a.ts", Line: 2}
	got := diag.Place(d, []diag.Note{
		{Path: "a.ts", Line: 2, Message: "a hint", Severity: diag.Hint},
		{Path: "a.ts", Line: 2, Message: "an error", Severity: diag.Error},
		{Path: "a.ts", Line: 2, Message: "a warning", Severity: diag.Warning},
	})

	want := []string{"an error", "a warning", "a hint"}
	for i, n := range got.On[at] {
		if n.Message != want[i] {
			t.Errorf("note %d is %q, want %q", i, n.Message, want[i])
		}
	}
}

// A deleted file has no after side for a checker to answer about, and asking
// one about it is a subprocess spent on a file that is not there.
func TestFilesSkipsWhatHasNoAfterSide(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(patch + `diff --git a/bin.png b/bin.png
Binary files a/bin.png and b/bin.png differ
`))

	got := diag.Files(d)
	want := []string{"a.ts", "gone.ts"}

	if len(got) != len(want) {
		t.Fatalf("files are %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("file %d is %q, want %q", i, got[i], want[i])
		}
	}
}
