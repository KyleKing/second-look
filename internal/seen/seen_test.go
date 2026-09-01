package seen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/diff"
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
// makes read-state survive a force-push.
func TestIdentityFollowsContentNotPosition(t *testing.T) {
	t.Parallel()

	base := diff.Parse([]byte(patch))
	want := seen.Hunk(base, "internal/a.go", 1)

	tests := []struct {
		name  string
		patch string
		same  bool
	}{
		{"unchanged", patch, true},
		{"line numbers shifted", strings.Replace(patch, "@@ -1,3 +1,4 @@", "@@ -41,3 +58,4 @@", 1), true},
		{"a line changed", strings.Replace(patch, "+// added one", "+// added ONE", 1), false},
		{"a context line changed", strings.Replace(patch, " func A() {}", " func A(x int) {}", 1), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := seen.Hunk(diff.Parse([]byte(tc.patch)), "internal/a.go", 1)
			if same := got == want; same != tc.same {
				t.Errorf("identity held = %v, want %v", same, tc.same)
			}
		})
	}
}

// The same text in two files is two hunks: a change read in one place has not
// been read in the other, where it sits in different code.
func TestIdentityIncludesTheFile(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(strings.ReplaceAll(patch, "added three", "added one")))
	if seen.Hunk(d, "internal/a.go", 1) == seen.Hunk(d, "internal/b.go", 3) {
		t.Error("identical text in two files answers one identity")
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

	path := filepath.Join(t.TempDir(), "pr-2.toml")

	set, err := seen.Load(path)
	if err != nil {
		t.Fatal(err)
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

// A review nobody has opened has no file, which is the normal first case and
// not an error.
func TestLoadOfNothingIsEmpty(t *testing.T) {
	t.Parallel()

	set, err := seen.Load(seen.Path(t.TempDir(), 2))
	if err != nil {
		t.Fatalf("loading a set that was never written: %v", err)
	}

	if set.Count(seen.Hunks(diff.Parse([]byte(patch)))) != 0 {
		t.Error("an unwritten set came back non-empty")
	}
}
