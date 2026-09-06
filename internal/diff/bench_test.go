package diff_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/diff"
)

// openSizes are three points on the size a real review opens at: a small
// fix-sized diff (this repository's median commit is 170 changed lines),
// this repository's own largest historical commit (32 files, 2,198
// insertions), and a lockfile-scale change. Two local package-lock.json
// files on disk (a full regeneration rewrites nearly every line) run
// 1,100-4,000 lines in one file; a dependency bump that also touches a
// monorepo's lockfiles, or a sweeping rename, spreads that across many
// files rather than one, which is the shape the large case targets.
var openSizes = []struct {
	name  string
	files int
	lines int
}{
	{"small", 5, 40},
	{"medium", 32, 70},
	{"large", 100, 200},
}

// bigPatch builds a patch of files added-only files, each lines long, the
// worst case for a hunk: no context line lets it split.
func bigPatch(files, lines int) []byte {
	var patch strings.Builder

	for f := range files {
		path := fmt.Sprintf("internal/pkg%02d/file.go", f)
		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n",
			path, path, path, path, lines, lines)

		for l := range lines {
			fmt.Fprintf(&patch, "+\tline %d of a change worth reading\n", l)
		}
	}

	return []byte(patch.String())
}

func BenchmarkParse(b *testing.B) {
	for _, sz := range openSizes {
		b.Run(sz.name, func(b *testing.B) {
			patch := bigPatch(sz.files, sz.lines)
			b.ReportAllocs()

			for b.Loop() {
				_ = diff.Parse(patch)
			}
		})
	}
}

func BenchmarkRefine(b *testing.B) {
	for _, sz := range openSizes {
		b.Run(sz.name, func(b *testing.B) {
			d := diff.Parse(bigPatch(sz.files, sz.lines))
			b.ReportAllocs()

			for b.Loop() {
				_ = d.Refine()
			}
		})
	}
}
