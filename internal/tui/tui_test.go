package tui_test

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

const patch = `diff --git a/internal/vcs/diff.go b/internal/vcs/diff.go
index 1111111..2222222 100644
--- a/internal/vcs/diff.go
+++ b/internal/vcs/diff.go
@@ -14,6 +14,7 @@ func Parse(r io.Reader) ([]Hunk, error) {
 	first := 1
-	lines := split(r)
+	lines, err := split(r)
+	if err != nil {
 	last := 4
diff --git a/internal/vcs/git.go b/internal/vcs/git.go
index 3333333..4444444 100644
--- a/internal/vcs/git.go
+++ b/internal/vcs/git.go
@@ -200,3 +201,3 @@ func Head() string {
 	return head
`

const parsed = "internal/vcs/diff.go"

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func comment(id, path, side string, line int, body string) artifact.Comment {
	return artifact.Comment{
		ID: id, Path: path, Side: side, Line: line, Body: body,
		Severity: "major", Status: artifact.StatusReady,
	}
}

func fixture(t *testing.T, cs ...artifact.Comment) (*tui.Model, string) {
	t.Helper()

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: cs,
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	posted := func(context.Context, *artifact.Review) (string, error) { return "posted", nil }

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, posted)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m, path
}

// A comment renders under the line it anchors to, or the reader cannot tell
// which line it is about, which is the one thing the screen exists to show.
func TestCommentsRenderUnderTheirAnchor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		c     artifact.Comment
		above string
	}{
		{"added line", comment("c1", parsed, artifact.SideRight, 15, "on the add"), "lines, err := split(r)"},
		{"removed line", comment("c2", parsed, artifact.SideLeft, 15, "on the remove"), "lines := split(r)"},
		{"context line", comment("c3", parsed, artifact.SideRight, 14, "on the context"), "first := 1"},
		{"second file", comment("c4", "internal/vcs/git.go", artifact.SideRight, 201, "on the tail"), "return head"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, _ := fixture(t, tc.c)
			lines := strings.Split(plain(m.Frame()), "\n")

			at := -1

			for i, l := range lines {
				if strings.Contains(l, tc.c.Body) {
					at = i
				}
			}

			if at < 1 {
				t.Fatalf("the comment body never rendered:\n%s", strings.Join(lines, "\n"))
			}

			// Walk up off the comment block to the diff line it hangs from.
			for at > 0 && strings.Contains(lines[at], "\u2502 ") {
				at--
			}

			if !strings.Contains(lines[at], tc.above) {
				t.Errorf("the comment hangs from %q, want %q", lines[at], tc.above)
			}
		})
	}
}

// Staging refuses a comment the diff does not carry, so one reaching the screen
// means the diff moved underneath it. Hiding it would hide the problem.
func TestACommentOutsideTheDiffIsStillListed(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", "internal/gone.go", artifact.SideRight, 9, "orphaned finding"))

	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})

	got := plain(m.Frame())
	if !strings.Contains(got, "orphaned finding") || !strings.Contains(got, "not in this diff") {
		t.Errorf("an unanchored comment vanished:\n%s", got)
	}
}

func TestNoRowOverflowsTheFrame(t *testing.T) {
	t.Parallel()

	sizes := []struct{ w, h int }{{80, 24}, {100, 30}, {200, 50}}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()

			m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15,
				strings.Repeat("a very long sentence that has to wrap somewhere sensible ", 6)))
			m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})

			lines := strings.Split(plain(m.Frame()), "\n")
			if len(lines) != size.h {
				t.Errorf("rendered %d lines, want %d", len(lines), size.h)
			}

			for i, l := range lines {
				if n := len([]rune(l)); n > size.w {
					t.Errorf("line %d is %d wide, want at most %d: %q", i, n, size.w, l)
				}
			}
		})
	}
}

// Skipping is one keystroke, and the schema requires a reason, so the keystroke
// has to supply one or the review stops validating.
func TestSkippingFillsInAReason(t *testing.T) {
	t.Parallel()

	m, path := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "on the add"))

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if saved.Comments[0].Status != artifact.StatusSkip {
		t.Errorf("status = %q, want skip", saved.Comments[0].Status)
	}
	if saved.Comments[0].SkipReason == "" {
		t.Error("a skip was saved with no reason, which will not validate")
	}
	if err := saved.Validate(); err != nil {
		t.Errorf("the saved review no longer validates: %v", err)
	}
}

func TestTooSmallAFrameSaysSo(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})

	if got := plain(m.Frame()); !strings.Contains(got, "80x10") {
		t.Errorf("render = %q, want the minimum size", got)
	}
}
