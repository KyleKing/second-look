package diff_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/diff"
)

const patch = `diff --git a/internal/one.go b/internal/one.go
index 1111111..2222222 100644
--- a/internal/one.go
+++ b/internal/one.go
@@ -10,6 +10,7 @@ func one() {
 	first := 1
-	dropped := 2
+	added := 2
+	alsoAdded := 3
 	last := 4
diff --git a/internal/new.go b/internal/new.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/internal/new.go
@@ -0,0 +1,2 @@
+package internal
+
`

func TestAnchor(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(patch))

	tests := []struct {
		name string
		path string
		side string
		line int
		want string
		ok   bool
	}{
		{"context line on the right", "internal/one.go", diff.SideRight, 10, "\tfirst := 1", true},
		{"added line", "internal/one.go", diff.SideRight, 11, "\tadded := 2", true},
		{"second added line", "internal/one.go", diff.SideRight, 12, "\talsoAdded := 3", true},
		{"context after the additions", "internal/one.go", diff.SideRight, 13, "\tlast := 4", true},
		{"removed line on the left", "internal/one.go", diff.SideLeft, 11, "\tdropped := 2", true},
		{"a removed line is absent from the right", "internal/one.go", diff.SideRight, 993, "", false},
		{"a new file has no left side", "internal/new.go", diff.SideLeft, 1, "", false},
		{"first line of a new file", "internal/new.go", diff.SideRight, 1, "package internal", true},
		{"a path the diff never touched", "internal/absent.go", diff.SideRight, 1, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := d.Anchor(tc.path, tc.side, tc.line)
			if ok != tc.ok || got != tc.want {
				t.Errorf("Anchor(%q, %s, %d) = (%q, %v), want (%q, %v)",
					tc.path, tc.side, tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}
