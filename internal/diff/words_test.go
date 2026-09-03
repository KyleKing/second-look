package diff_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/diff"
)

// marked renders a line with its changed runs bracketed, which is what the
// screen draws as a band and what a test can read.
func marked(text string, spans []diff.Span) string {
	var (
		b  strings.Builder
		at int
	)

	for _, s := range spans {
		b.WriteString(text[at:s.From] + "[" + text[s.From:s.To] + "]")
		at = s.To
	}

	return b.String() + text[at:]
}

func TestRefineMarksOnlyWhatChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		before string
		after  string
		gone   string
		came   string
	}{
		{
			name:   "one identifier",
			before: "\tif err := run(ctx); err != nil {",
			after:  "\tif err := walk(ctx); err != nil {",
			gone:   "\tif err := [run](ctx); err != nil {",
			came:   "\tif err := [walk](ctx); err != nil {",
		},
		{
			name:   "an argument added",
			before: "return fmt.Errorf(\"reading: %w\", err)",
			after:  "return fmt.Errorf(\"reading %s: %w\", path, err)",
			gone:   "return fmt.Errorf(\"reading: %w\", err)",
			came:   "return fmt.Errorf(\"reading[ %s]: %w\", [path, ]err)",
		},
		{
			name:   "trailing text only",
			before: "const timeout = 5",
			after:  "const timeout = 30",
			gone:   "const timeout = [5]",
			came:   "const timeout = [30]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := diff.Parse([]byte(patchOf(tc.before, tc.after)))
			r := d.Refine()

			gone := r[diff.LineRef{Path: "a.go", Kind: diff.KindRemove, Old: 1}]
			came := r[diff.LineRef{Path: "a.go", Kind: diff.KindAdd, New: 1}]

			if got := marked(tc.before, gone); got != tc.gone {
				t.Errorf("removed line marked as\n  %s\nwant\n  %s", got, tc.gone)
			}

			if got := marked(tc.after, came); got != tc.came {
				t.Errorf("added line marked as\n  %s\nwant\n  %s", got, tc.came)
			}
		})
	}
}

// A line rewritten rather than edited is left whole: a mark covering most of it
// says less than the color already does.
func TestRefineLeavesARewriteWhole(t *testing.T) {
	t.Parallel()

	rewritten := patchOf("\tcount := len(items)", "\treturn fmt.Sprintf(\"%s/%s\", owner, repo)")

	d := diff.Parse([]byte(rewritten))

	if got := d.Refine(); len(got) != 0 {
		t.Errorf("a rewritten line was marked: %v", got)
	}
}

// Pairing is positional inside a change block, and a block ends at the first
// context line, so two unrelated edits in one hunk do not pair across the gap.
func TestRefinePairsWithinAChangeBlock(t *testing.T) {
	t.Parallel()

	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,4 +1,4 @@\n" +
		"-const a = 1\n+const a = 2\n context\n-const b = 3\n+const b = 4\n"

	r := diff.Parse([]byte(patch)).Refine()

	second := r[diff.LineRef{Path: "a.go", Kind: diff.KindRemove, Old: 3}]
	if got := marked("const b = 3", second); got != "const b = [3]" {
		t.Errorf("the second block marked as %q", got)
	}
}

func patchOf(before, after string) string {
	return "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n" +
		"-" + before + "\n+" + after + "\n"
}
