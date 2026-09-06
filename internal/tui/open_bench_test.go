package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/structure"
	"github.com/kyleking/second-look/internal/tui"
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

// review builds a prepared review over a diff of files added-only files,
// each lines long, with a comment on every other file, matching the shape
// big builds in bench_test.go.
func review(b *testing.B, files, lines int) (*artifact.Review, *diff.Diff) {
	b.Helper()

	var (
		patch    strings.Builder
		comments []artifact.Comment
	)

	for f := range files {
		path := fmt.Sprintf("internal/pkg%02d/file.go", f)
		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n",
			path, path, path, path, lines, lines)

		for l := range lines {
			fmt.Fprintf(&patch, "+\tline %d of a change worth reading\n", l)
		}

		if f%2 == 0 {
			comments = append(comments, artifact.Comment{
				ID: path, Path: path, Line: 1, Side: artifact.SideRight,
				Body:     strings.Repeat("a sentence about this line that runs on a while. ", 6),
				Severity: "major", Status: artifact.StatusReady,
			})
		}
	}

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "second-look", Number: 2,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: comments,
	}

	return r, diff.Parse([]byte(patch.String()))
}

func opened(b *testing.B, files, lines int) *tui.Model {
	b.Helper()

	r, d := review(b, files, lines)
	m := tui.New(b.Context(), r, d, b.TempDir()+"/pr-2.toml", nil)
	m.Init()

	return m
}

// BenchmarkOpenModel is New and Init together, the cost of standing the
// screen up before the terminal has sent a size or the structural pass has
// answered.
func BenchmarkOpenModel(b *testing.B) {
	for _, sz := range openSizes {
		b.Run(sz.name, func(b *testing.B) {
			r, d := review(b, sz.files, sz.lines)
			b.ReportAllocs()

			for b.Loop() {
				m := tui.New(b.Context(), r, d, b.TempDir()+"/pr-2.toml", nil)
				m.Init()
			}
		})
	}
}

// BenchmarkOpenRebuild is laying out rows for the first frame: the pass a
// fold, toggle, or order change also runs, over the whole diff rather than
// only the visible window.
func BenchmarkOpenRebuild(b *testing.B) {
	for _, sz := range openSizes {
		b.Run(sz.name, func(b *testing.B) {
			m := opened(b, sz.files, sz.lines)
			b.ReportAllocs()

			for b.Loop() {
				m.Relayout()
			}
		})
	}
}

// BenchmarkOpenFrame renders one frame at the two widths a review commonly
// opens at, off the plain renderer's cache so what is measured is the layout
// walk rather than a first-time chroma lex (BenchmarkRichFrame in
// bench_test.go already isolates that cost).
func BenchmarkOpenFrame(b *testing.B) {
	for _, sz := range openSizes {
		for _, width := range []int{80, 120} {
			b.Run(fmt.Sprintf("%s/%d", sz.name, width), func(b *testing.B) {
				m := opened(b, sz.files, sz.lines)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
				b.ReportAllocs()

				for b.Loop() {
					_ = m.Frame()
				}
			})
		}
	}
}

// BenchmarkOpenStructural is the re-layout that runs when the structural pass
// answers behind the first frame: one ast-grep subprocess per hunk side,
// capped at 8 concurrent, followed by the same rebuild BenchmarkOpenRebuild
// measures alone.
func BenchmarkOpenStructural(b *testing.B) {
	if !structure.Available() {
		b.Skip("ast-grep is not installed")
	}

	for _, sz := range openSizes {
		b.Run(sz.name, func(b *testing.B) {
			m := opened(b, sz.files, sz.lines)
			b.ReportAllocs()

			for b.Loop() {
				m.Restructure()
			}
		})
	}
}
