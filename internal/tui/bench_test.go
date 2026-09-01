package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// big is a review the size of a refactor: 40 files of 200 lines each with a
// comment every other file. Rebuilding runs on every resize event and on every
// keystroke that changes a comment, so it is the one path that cannot be slow.
func big(b *testing.B) *tui.Model {
	b.Helper()

	var (
		patch    strings.Builder
		comments []artifact.Comment
	)

	for f := range 40 {
		path := fmt.Sprintf("internal/pkg%02d/file.go", f)
		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1,200 +1,200 @@\n", path, path, path, path)

		for l := range 200 {
			fmt.Fprintf(&patch, "+\tline %d of a change worth reading\n", l)
		}

		if f%2 == 0 {
			comments = append(comments, artifact.Comment{
				ID: path, Path: path, Line: 50, Side: artifact.SideRight,
				Body:     strings.Repeat("a sentence about this line that runs on a while. ", 6),
				Severity: "major", Status: artifact.StatusReady,
			})
		}
	}

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "second-look", Number: 2,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: comments,
	}

	m := tui.New(b.Context(), r, diff.Parse([]byte(patch.String())), b.TempDir()+"/pr-2.toml", nil)
	m.Init()

	return m
}

func BenchmarkResize(b *testing.B) {
	m := big(b)

	for i := 0; b.Loop(); i++ {
		m.Update(tea.WindowSizeMsg{Width: 80 + i%40, Height: 24})
	}
}

func BenchmarkFrame(b *testing.B) {
	m := big(b)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	for b.Loop() {
		_ = m.Frame()
	}
}
