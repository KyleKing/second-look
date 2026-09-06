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
	return slices.Concat(
		goCases(),
		tomlCases(),
		npmCases(),
		pnpmCases(),
		yarnCases(),
		gemfileCases(),
	)
}

func goCases() []readCase {
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
	}
}

func tomlCases() []readCase {
	return []readCase{
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
	}
}

func npmCases() []readCase {
	return []readCase{
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

func pnpmCases() []readCase {
	return []readCase{
		{
			name: "pnpm-lock.yaml scoped key updated",
			patch: `diff --git a/pnpm-lock.yaml b/pnpm-lock.yaml
--- a/pnpm-lock.yaml
+++ b/pnpm-lock.yaml
@@ -1,4 +1,4 @@
 packages:
-  '@babel/core@7.19.0':
+  '@babel/core@7.20.0':
     resolution: {integrity: sha512-xyz=}
`,
			want:   []meta.Row{{Name: "@babel/core", From: "7.19.0", To: "7.20.0"}},
			wantOk: true,
		},
		{
			name: "pnpm-lock.yaml classic key added",
			patch: `diff --git a/pnpm-lock.yaml b/pnpm-lock.yaml
--- a/pnpm-lock.yaml
+++ b/pnpm-lock.yaml
@@ -1,3 +1,6 @@
 packages:
   /lodash@4.17.21:
     resolution: {integrity: sha512-abc=}
+
+  /left-pad@1.3.0:
+    resolution: {integrity: sha512-xyz=}
`,
			want:   []meta.Row{{Name: "left-pad", From: "", To: "1.3.0"}},
			wantOk: true,
		},
		{
			name: "pnpm-lock.yaml classic key removed",
			patch: `diff --git a/pnpm-lock.yaml b/pnpm-lock.yaml
--- a/pnpm-lock.yaml
+++ b/pnpm-lock.yaml
@@ -1,7 +1,4 @@
 packages:
-  /old-pkg@1.0.0:
-    resolution: {integrity: sha512-old=}
-
   /keep@2.0.0:
     resolution: {integrity: sha512-keep=}
`,
			want:   []meta.Row{{Name: "old-pkg", From: "1.0.0", To: ""}},
			wantOk: true,
		},
	}
}

func yarnCases() []readCase {
	return []readCase{
		{
			name: "yarn.lock scoped header updated",
			patch: `diff --git a/yarn.lock b/yarn.lock
--- a/yarn.lock
+++ b/yarn.lock
@@ -1,4 +1,4 @@
 "@babel/core@^7.19.0", "@babel/core@^7.20.0":
-  version "7.19.0"
+  version "7.20.0"
   resolved "https://registry.yarnpkg.com/@babel/core/-/core-7.20.0.tgz"
`,
			want:   []meta.Row{{Name: "@babel/core", From: "7.19.0", To: "7.20.0"}},
			wantOk: true,
		},
		{
			name: "yarn.lock entry added",
			patch: `diff --git a/yarn.lock b/yarn.lock
--- a/yarn.lock
+++ b/yarn.lock
@@ -1,3 +1,7 @@
 lodash@^4.17.0:
   version "4.17.21"
   resolved "https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz"
+
+left-pad@^1.0.0:
+  version "1.3.0"
+  resolved "https://registry.yarnpkg.com/left-pad/-/left-pad-1.3.0.tgz"
`,
			want:   []meta.Row{{Name: "left-pad", From: "", To: "1.3.0"}},
			wantOk: true,
		},
		{
			name: "yarn.lock entry removed",
			patch: `diff --git a/yarn.lock b/yarn.lock
--- a/yarn.lock
+++ b/yarn.lock
@@ -1,7 +1,3 @@
-old-pkg@^1.0.0:
-  version "1.0.0"
-  resolved "https://registry.yarnpkg.com/old-pkg/-/old-pkg-1.0.0.tgz"
-
 keep@^2.0.0:
   version "2.0.0"
   resolved "https://registry.yarnpkg.com/keep/-/keep-2.0.0.tgz"
`,
			want:   []meta.Row{{Name: "old-pkg", From: "1.0.0", To: ""}},
			wantOk: true,
		},
	}
}

func gemfileCases() []readCase {
	return []readCase{
		{
			name: "Gemfile.lock spec version updated",
			patch: `diff --git a/Gemfile.lock b/Gemfile.lock
--- a/Gemfile.lock
+++ b/Gemfile.lock
@@ -1,6 +1,6 @@
 GEM
   remote: https://rubygems.org/
   specs:
-    concurrent-ruby (1.2.2)
+    concurrent-ruby (1.2.3)
     i18n (1.14.1)
       concurrent-ruby (~> 1.0)
`,
			want:   []meta.Row{{Name: "concurrent-ruby", From: "1.2.2", To: "1.2.3"}},
			wantOk: true,
		},
		{
			name: "Gemfile.lock spec added",
			patch: `diff --git a/Gemfile.lock b/Gemfile.lock
--- a/Gemfile.lock
+++ b/Gemfile.lock
@@ -2,6 +2,9 @@
   remote: https://rubygems.org/
   specs:
     concurrent-ruby (1.2.2)
+    i18n (1.14.1)
+      concurrent-ruby (~> 1.0)
 
 PLATFORMS
   ruby
`,
			want:   []meta.Row{{Name: "i18n", From: "", To: "1.14.1"}},
			wantOk: true,
		},
		{
			name: "Gemfile.lock spec removed",
			patch: `diff --git a/Gemfile.lock b/Gemfile.lock
--- a/Gemfile.lock
+++ b/Gemfile.lock
@@ -2,7 +2,5 @@
   remote: https://rubygems.org/
   specs:
     concurrent-ruby (1.2.2)
-    i18n (1.14.1)
-      concurrent-ruby (~> 1.0)
 
 PLATFORMS
   ruby
`,
			want:   []meta.Row{{Name: "i18n", From: "1.14.1", To: ""}},
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
