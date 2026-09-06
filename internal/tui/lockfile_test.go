package tui_test

import (
	"strings"
	"testing"
)

const goSumPatch = `diff --git a/go.sum b/go.sum
index 1111111..2222222 100644
--- a/go.sum
+++ b/go.sum
@@ -1,5 +1,5 @@
 example.com/kept v1.0.0 h1:mmmmmmmm=
-example.com/moved v0.12.0 h1:cccccccc=
-example.com/moved v0.12.0/go.mod h1:dddddddd=
-example.com/left v0.0.20 h1:eeeeeeee=
+example.com/moved v0.13.0 h1:iiiiiiii=
+example.com/moved v0.13.0/go.mod h1:jjjjjjjj=
+example.com/came v0.16.0 h1:kkkkkkkk=
`

const minifiedPatch = `diff --git a/web/app.min.js b/web/app.min.js
index 1111111..2222222 100644
--- a/web/app.min.js
+++ b/web/app.min.js
@@ -1,2 +1,2 @@
-var a=1,b=2;
+var a=1,b=3;
 var c=4;
`

// A lockfile is folded like anything else a machine wrote, and what it says
// while folded is the difference: the dependencies it changed rather than a
// hunk count, and never the hashes, which is the reading the fold exists to
// save.
func TestFoldedGeneratedFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		patch  string
		want   []string
		unwant []string
	}{
		{
			name:  "a lockfile reads as its dependencies",
			patch: goSumPatch,
			want: []string{
				"3 dependencies",
				"example.com/moved", "v0.12.0 → v0.13.0",
				"example.com/came", "added v0.16.0",
				"example.com/left", "removed v0.0.20",
			},
			unwant: []string{"h1:", "example.com/kept"},
		},
		{
			name:   "a format nothing here reads keeps counting hunks",
			patch:  minifiedPatch,
			want:   []string{"1 hunk folded"},
			unwant: []string{"var a=1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, _, _ := fixtureWith(t, tc.patch)
			frame := plain(m.Frame())

			for _, want := range tc.want {
				if !strings.Contains(frame, want) {
					t.Errorf("the frame does not carry %q:\n%s", want, frame)
				}
			}

			for _, unwant := range tc.unwant {
				if strings.Contains(frame, unwant) {
					t.Errorf("the frame carries %q and should not:\n%s", unwant, frame)
				}
			}
		})
	}
}
