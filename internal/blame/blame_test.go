package blame_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/blame"
	"github.com/kyleking/second-look/internal/diff"
)

// The patch covers one hunk with an old side, one file that only adds, and a
// rename, so which path each side is asked about and answered under is pinned.
const patch = `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -10,4 +10,5 @@ func main() {
 	one
 	two
-	three
+	three!
+	four
 	five
diff --git a/added.go b/added.go
new file mode 100644
index 0000000..9999999
--- /dev/null
+++ b/added.go
@@ -0,0 +1,2 @@
+package new
+
diff --git a/old.go b/new.go
similarity index 90%
rename from old.go
rename to new.go
index aaaaaaa..bbbbbbb 100644
--- a/old.go
+++ b/new.go
@@ -7,2 +7,2 @@
 	before
-after
+later
`

// recorder answers every line of every range it is asked, and remembers what
// it was asked so the test can check the path and the spans.
type recorder struct {
	mu    sync.Mutex
	calls int
	asked map[string][]vcs.LineRange
}

func (r *recorder) query(_ context.Context, path string, ranges []vcs.LineRange) ([]vcs.BlameLine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++
	if r.asked == nil {
		r.asked = map[string][]vcs.LineRange{}
	}
	r.asked[path] = append(r.asked[path], ranges...)

	var out []vcs.BlameLine

	for _, rg := range ranges {
		for line := rg.From; line <= rg.To; line++ {
			out = append(out, vcs.BlameLine{
				Line: line, Commit: "abc", Author: "test",
				When: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			})
		}
	}

	return out, nil
}

func TestRead(t *testing.T) {
	t.Parallel()

	rec := &recorder{}

	got, err := blame.Read(t.Context(), rec.query, diff.Parse([]byte(patch)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// main.go's hunk spans old lines 10-13: the three context lines and the
	// removed one. The added lines have no old side to blame.
	if want := []vcs.LineRange{{From: 10, To: 13}}; !slices.Equal(rec.asked["main.go"], want) {
		t.Errorf("main.go asked %v, want %v", rec.asked["main.go"], want)
	}

	lines, ok := got["main.go"]
	if !ok {
		t.Fatal("main.go has no blame")
	}
	for _, line := range []int{10, 11, 12, 13} {
		if _, ok := lines[line]; !ok {
			t.Errorf("line %d has no attribution", line)
		}
	}

	// A rename is blamed at the name the base knew it under and answered at
	// the name the rows are drawn with.
	if want := []vcs.LineRange{{From: 7, To: 8}}; !slices.Equal(rec.asked["old.go"], want) {
		t.Errorf("rename asked %v under old.go, want %v", rec.asked["old.go"], want)
	}
	if _, ok := rec.asked["new.go"]; ok {
		t.Error("the rename's new path was asked about; the base knows the old one")
	}
	if _, ok := got["new.go"][8]; !ok {
		t.Error("line 8 of new.go, the removed 'after', has no attribution")
	}

	// A file that only adds has no old side, so it asks nothing.
	if _, ok := rec.asked["added.go"]; ok {
		t.Error("added.go, which has no old side, was asked about")
	}
	if _, ok := got["added.go"]; ok {
		t.Error("added.go has blame it never could have had")
	}
}

// A second read of the same diff asks nothing, because the first answer is
// kept under the old side's blob. A diff whose writer named no objects has no
// key to keep it under, so it asks every time.
func TestReadCached(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	d := diff.Parse([]byte(patch))
	rec := &recorder{}

	read := func() (blame.Map, error) {
		return blame.Read(t.Context(), blame.Cached(root, rec.query, d), d)
	}

	first, err := read()
	if err != nil {
		t.Fatalf("first cached read: %v", err)
	}
	if rec.calls != 2 {
		t.Fatalf("first pass made %d calls, want 2 (main.go and the rename's old side)", rec.calls)
	}

	second, err := read()
	if err != nil {
		t.Fatalf("second cached read: %v", err)
	}
	if rec.calls != 2 {
		t.Errorf("the second pass asked again; the blob-keyed answers were already kept")
	}
	if len(second["main.go"]) != len(first["main.go"]) {
		t.Errorf("cached read gave %d lines for main.go, want %d", len(second["main.go"]), len(first["main.go"]))
	}

	// No index line, no blob, no place to keep the answer.
	rec.calls = 0

	const blobless = "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n"

	less := diff.Parse([]byte(blobless))
	for range 2 {
		if _, err := blame.Read(t.Context(), blame.Cached(root, rec.query, less), less); err != nil {
			t.Fatalf("blobless cached read: %v", err)
		}
	}
	if rec.calls != 2 {
		t.Errorf("a diff naming no blobs made %d calls, want 2", rec.calls)
	}
}

var errBoom = errors.New("boom")

func TestReadFailsLoudly(t *testing.T) {
	t.Parallel()

	_, err := blame.Read(t.Context(), func(_ context.Context, _ string, _ []vcs.LineRange) ([]vcs.BlameLine, error) {
		return nil, errBoom
	}, diff.Parse([]byte(patch)))
	if err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("Read error = %v, want boom", err)
	}
	if !strings.Contains(err.Error(), "main.go") && !strings.Contains(err.Error(), "old.go") {
		t.Fatalf("Read error = %v, want it to name the file it failed on", err)
	}
}
