package seen_test

import (
	"strings"
	"testing"

	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/seen"
)

const patch = `diff --git a/internal/a.go b/internal/a.go
--- a/internal/a.go
+++ b/internal/a.go
@@ -1,3 +1,4 @@
 package a
+// added one
 func A() {}
@@ -20,3 +21,4 @@
 func B() {}
+// added two
 func C() {}
diff --git a/internal/b.go b/internal/b.go
--- a/internal/b.go
+++ b/internal/b.go
@@ -1,2 +1,3 @@
 package b
+// added three
`

// Identity is content, not position. A hunk that slides down the file because
// something above it grew is the same hunk and stays read; a hunk whose text
// changed is a different one and comes back unread. That is the whole of what
// makes read-state survive a force-push. The same text in two files is two
// hunks: a change read in one place has not been read in the other.
func TestIdentityFollowsContentNotPosition(t *testing.T) {
	t.Parallel()

	base := diff.Parse([]byte(patch))
	want := seen.Hunk(base, "internal/a.go", 1)

	tests := []struct {
		name  string
		patch string
		path  string
		hunk  int
		same  bool
	}{
		{"unchanged", patch, "internal/a.go", 1, true},
		{
			"line numbers shifted",
			strings.Replace(patch, "@@ -1,3 +1,4 @@", "@@ -41,3 +58,4 @@", 1),
			"internal/a.go",
			1,
			true,
		},
		{"a line changed", strings.Replace(patch, "+// added one", "+// added ONE", 1), "internal/a.go", 1, false},
		{
			"a context line changed",
			strings.Replace(patch, " func A() {}", " func A(x int) {}", 1),
			"internal/a.go",
			1,
			false,
		},
		{"another hunk of the file", patch, "internal/a.go", 2, false},
		{
			"the same text in another file",
			strings.ReplaceAll(patch, "added three", "added one"),
			"internal/b.go",
			3,
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := seen.Hunk(diff.Parse([]byte(tc.patch)), tc.path, tc.hunk)
			if same := got == want; same != tc.same {
				t.Errorf("identity held = %v, want %v", same, tc.same)
			}
		})
	}
}

// A mark for a hunk the diff no longer carries is dead weight, and a file that
// only grows is one nobody can read.
func TestSaveKeepsOnlyLiveHunks(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(patch))
	refs := seen.Hunks(d)

	if len(refs) != 3 {
		t.Fatalf("found %d hunks, want 3", len(refs))
	}

	path := seen.Path(t.TempDir(), 2)

	// A review nobody has opened has no file, which is the normal first case
	// and not an error.
	set, err := seen.Load(path)
	if err != nil {
		t.Fatalf("loading a set that was never written: %v", err)
	}

	if set.Count(refs) != 0 {
		t.Error("an unwritten set came back non-empty")
	}

	set.Mark(true, refs[0].ID, refs[1].ID, "a hunk that has since gone")

	if err := seen.Save(path, set, refs); err != nil {
		t.Fatal(err)
	}

	back, err := seen.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := back.Count(refs); got != 2 {
		t.Errorf("%d hunks came back read, want 2", got)
	}

	if back.Has("a hunk that has since gone") {
		t.Error("a mark for a hunk the diff dropped was kept")
	}
}
