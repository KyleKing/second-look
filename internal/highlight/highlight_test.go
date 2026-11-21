package highlight_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/highlight"
)

// classed renders a line with each span bracketed by its class, which is what
// the screen colors and what a test can read.
func classed(text string, spans []highlight.Span) string {
	names := map[highlight.Class]string{
		highlight.Comment: "c", highlight.Keyword: "k", highlight.String: "s",
		highlight.Number: "n", highlight.Name: "i", highlight.Type: "t",
		highlight.Punctuation: "p", highlight.Function: "f",
	}

	var (
		b  strings.Builder
		at int
	)

	for _, s := range spans {
		b.WriteString(text[at:s.From] + names[s.Class] + "[" + text[s.From:s.To] + "]")
		at = s.To
	}

	return b.String() + text[at:]
}

// TestHighlight lexes by file name or by a fence's tag. A whole slice is lexed
// as one, because a line inside a raw string reads as code on its own and the
// state that says otherwise is on the line above it; a path or a tag no grammar
// answers for draws as plain text rather than as whatever chroma guesses.
func TestHighlight(t *testing.T) {
	t.Parallel()

	goSrc := []string{
		"// count the entries",
		"const limit = 5",
		`var name = "second-look"`,
	}
	goWant := []string{
		"c[// count the entries]",
		"k[const] i[limit] p[=] n[5]",
		"k[var] i[name] p[=] s[\"second-look\"]",
	}

	for _, tc := range []struct {
		name string
		got  [][]highlight.Span
		src  []string
		want []string
	}{
		{"by file name", highlight.Lines("internal/rate/rate.go", goSrc), goSrc, goWant},
		{"by a fence's tag", highlight.Tagged("go", goSrc), goSrc, goWant},
		{
			"across lines",
			highlight.Lines("a.go", []string{"var q = `", "func not() {}", "`"}),
			[]string{"var q = `", "func not() {}", "`"},
			[]string{"k[var] i[q] p[=] s[`]", "s[func not() {}]", "s[`]"},
		},
		{"an unknown extension", highlight.Lines("data/records.unknownext", goSrc), goSrc, nil},
		{"an untagged fence", highlight.Tagged("", goSrc), goSrc, nil},
		{"a tag no grammar answers", highlight.Tagged("no-such-language", goSrc), goSrc, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.want == nil {
				if tc.got != nil {
					t.Errorf("answered %v, want nothing", tc.got)
				}

				return
			}

			if len(tc.got) != len(tc.want) {
				t.Fatalf("lexed %d lines, want %d", len(tc.got), len(tc.want))
			}

			for i, want := range tc.want {
				if line := classed(tc.src[i], tc.got[i]); line != want {
					t.Errorf("line %d lexed as\n  %s\nwant\n  %s", i, line, want)
				}
			}
		})
	}
}
