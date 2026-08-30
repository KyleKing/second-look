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

// series is what `gh pr diff --patch` returns: one diff per commit, so a file
// touched twice appears twice and the second entry renumbers it.
const series = `From aaaa Mon Sep 17 00:00:00 2001
Subject: [PATCH 1/2] first

diff --git a/internal/one.go b/internal/one.go
--- a/internal/one.go
+++ b/internal/one.go
@@ -10,2 +10,3 @@ func one() {
 	first := 1
+	added := 2
 	last := 4
From bbbb Mon Sep 17 00:00:00 2001
Subject: [PATCH 2/2] second

diff --git a/internal/one.go b/internal/one.go
--- a/internal/one.go
+++ b/internal/one.go
@@ -8,2 +8,3 @@ func one() {
 	zeroth := 0
+	inserted := 1
 	first := 1
`

func TestRepeated(t *testing.T) {
	t.Parallel()

	if got := diff.Parse([]byte(patch)).Repeated(); len(got) != 0 {
		t.Errorf("Repeated() = %v, want none", got)
	}

	got := diff.Parse([]byte(series)).Repeated()
	if len(got) != 1 || got[0] != "internal/one.go" {
		t.Errorf("Repeated() = %v, want [internal/one.go]", got)
	}
}

// sqlPatch removes a SQL comment, whose text starts with "-- ", so the patch
// line reads "--- " and looks exactly like a file header.
const sqlPatch = `diff --git a/db/up.sql b/db/up.sql
index 1111111..2222222 100644
--- a/db/up.sql
+++ b/db/up.sql
@@ -1,3 +1,3 @@
 create table t (id int);
--- drop the old one
+-- keep the old one
 select 1;
`

func TestParse_HunkContentIsNotAFileHeader(t *testing.T) {
	t.Parallel()

	d := diff.Parse([]byte(sqlPatch))

	tests := []struct {
		name string
		side string
		line int
		want string
	}{
		{"the removed comment keeps its pre-image number", diff.SideLeft, 2, "-- drop the old one"},
		{"the line after it is not pulled up", diff.SideLeft, 3, "select 1;"},
		{"the post-image is unshifted", diff.SideRight, 3, "select 1;"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := d.Anchor("db/up.sql", tc.side, tc.line)
			if !ok || got != tc.want {
				t.Errorf("Anchor(db/up.sql, %s, %d) = (%q, %v), want (%q, true)",
					tc.side, tc.line, got, ok, tc.want)
			}
		})
	}
}
