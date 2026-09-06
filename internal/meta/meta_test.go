package meta_test

import (
	"slices"
	"testing"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/meta"
)

type readCase struct {
	name   string
	patch  string
	want   []meta.Row
	wantOk bool
}

func readCases() []readCase {
	return []readCase{
		{
			name: "an unrecognized file",
			patch: `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,1 +1,1 @@
-old text
+new text
`,
			want:   nil,
			wantOk: false,
		},
		{
			name: "go.sum update collapses the /go.mod duplicate",
			patch: `diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -1,3 +1,3 @@
-github.com/foo/bar v1.0.0 h1:abc=
-github.com/foo/bar v1.0.0/go.mod h1:def=
+github.com/foo/bar v1.1.0 h1:xyz=
+github.com/foo/bar v1.1.0/go.mod h1:uvw=
 github.com/baz/qux v2.0.0 h1:same=
`,
			want:   []meta.Row{{Name: "github.com/foo/bar", From: "v1.0.0", To: "v1.1.0"}},
			wantOk: true,
		},
		{
			name: "go.mod require block add and remove",
			patch: `diff --git a/go.mod b/go.mod
--- a/go.mod
+++ b/go.mod
@@ -1,4 +1,4 @@
 require (
-	github.com/old/pkg v1.0.0
+	github.com/new/pkg v2.0.0
 	github.com/keep/pkg v3.0.0
 )
`,
			want: []meta.Row{
				{Name: "github.com/new/pkg", From: "", To: "v2.0.0"},
				{Name: "github.com/old/pkg", From: "v1.0.0", To: ""},
			},
			wantOk: true,
		},
		{
			name: "Cargo.lock version update",
			patch: `diff --git a/Cargo.lock b/Cargo.lock
--- a/Cargo.lock
+++ b/Cargo.lock
@@ -10,7 +10,7 @@
 [[package]]
 name = "serde"
-version = "1.0.150"
+version = "1.0.160"
 source = "registry+index"
`,
			want:   []meta.Row{{Name: "serde", From: "1.0.150", To: "1.0.160"}},
			wantOk: true,
		},
		{
			name: "uv.lock package added",
			patch: `diff --git a/uv.lock b/uv.lock
--- a/uv.lock
+++ b/uv.lock
@@ -20,6 +20,10 @@
 [[package]]
 name = "existing"
 version = "1.0.0"
+
+[[package]]
+name = "newpkg"
+version = "0.1.0"
`,
			want:   []meta.Row{{Name: "newpkg", From: "", To: "0.1.0"}},
			wantOk: true,
		},
		{
			name: "package-lock.json package removed",
			patch: `diff --git a/package-lock.json b/package-lock.json
--- a/package-lock.json
+++ b/package-lock.json
@@ -5,9 +5,5 @@
     "node_modules/keep": {
       "version": "1.0.0"
     },
-    "node_modules/gone": {
-      "version": "2.0.0"
-    },
     "node_modules/other": {
       "version": "3.0.0"
     }
`,
			want:   []meta.Row{{Name: "gone", From: "2.0.0", To: ""}},
			wantOk: true,
		},
	}
}

func TestRead(t *testing.T) {
	t.Parallel()

	for _, tc := range readCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := diff.Parse([]byte(tc.patch))
			if len(d.Files) != 1 {
				t.Fatalf("Parse() produced %d files, want 1", len(d.Files))
			}

			got, ok := meta.Read(&d.Files[0])
			if ok != tc.wantOk {
				t.Fatalf("Read() ok = %v, want %v", ok, tc.wantOk)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("Read() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
