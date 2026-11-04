package tui_test

import (
	"strings"
	"testing"
)

// A block whose best-match pairs skip around — an `if` pairing with the
// assignment that replaced it, and a tail pairing with a line two rows down —
// used to draw its unpaired removals ahead of every addition, so the gutter
// read 200, 201, 199, 200, 201, 202. Cut lines belong where they were cut:
// just ahead of the next row that kept a line from the old text.
func TestUnpairedCutsDrawWhereTheyWereCut(t *testing.T) {
	t.Parallel()

	m, _, _ := fixtureWith(t, `diff --git a/x/entity.py b/x/entity.py
index 1111111..2222222 100644
--- a/x/entity.py
+++ b/x/entity.py
@@ -196,8 +196,8 @@
         if contexts is None:
             return asscn
-        if asscn.contexts is None:
-            asscn.contexts = [c.model_dump(exclude_none=True) for c in contexts]
-        else:
-            asscn.contexts.extend([c.model_dump(exclude_none=True) for c in contexts])
+        asscn.contexts = [
+            *(asscn.contexts or []),
+            *(c.model_dump(exclude_none=True) for c in contexts),
+        ]
         session.add(asscn)
         return asscn
`)

	frame := plain(m.Frame())
	at := 0

	for _, want := range []string{
		"if asscn.contexts",                  // merged row: the if is gone, the assignment stayed
		"*(asscn.contexts or []),",           // an added line nothing pairs with
		"= [c.model_dump(exclude_none=True)", // the cut assignment, where it was cut
		"else:",                              // the cut else, beside it
		"extend",                             // the next merged row
	} {
		found := strings.Index(frame[at:], want)
		if found < 0 {
			t.Fatalf("the frame lost %q after column %d:\n%s", want, at, frame)
		}
		at += found + len(want)
	}
}
