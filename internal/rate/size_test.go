package rate_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/structure"
)

const sized = `diff --git a/one.go b/one.go
--- a/one.go
+++ b/one.go
@@ -1,3 +1,4 @@
 package one
-func a() {}
+func a(ctx context.Context) {}
+func b() {}
@@ -10,2 +11,2 @@
 func c() {
-	return
+		return
 }
`

// Size is counted off the same pass the rating is made from, and a hunk that
// changed nothing but layout counts toward neither side. A size that counted
// one would say a re-indent over forty files is the largest thing in the queue.
func TestSizeLeavesOutWhatChangedNoCode(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(sized))

	refs := seen.Hunks(d)
	if len(refs) != 2 {
		t.Fatalf("parsed %d hunks, want 2", len(refs))
	}

	readings := []structure.Reading{{Change: structure.ChangeCode}, {Change: structure.ChangeLayout}}

	got := rate.Measure(d, readings, refs)
	if got != (rate.Size{Added: 2, Removed: 1}) {
		t.Errorf("measured %+v, want the first hunk alone", got)
	}
}
