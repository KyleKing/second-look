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

func TestLinesClassifiesByGrammar(t *testing.T) {
	t.Parallel()

	src := []string{
		"// count the entries",
		"const limit = 5",
		`var name = "second-look"`,
	}

	got := highlight.Lines("internal/rate/rate.go", src)
	if len(got) != len(src) {
		t.Fatalf("lexed %d lines, want %d", len(got), len(src))
	}

	for i, want := range []string{
		"c[// count the entries]",
		"k[const] i[limit] p[=] n[5]",
		"k[var] i[name] p[=] s[\"second-look\"]",
	} {
		if line := classed(src[i], got[i]); line != want {
			t.Errorf("line %d lexed as\n  %s\nwant\n  %s", i, line, want)
		}
	}
}

// A line is not a program: one inside a raw string reads as code on its own,
// and the state that says otherwise is on the line above it.
func TestLinesCarryStateBetweenThem(t *testing.T) {
	t.Parallel()

	got := highlight.Lines("a.go", []string{"var q = `", "func not() {}", "`"})

	if len(got) != 3 {
		t.Fatalf("lexed %d lines, want 3", len(got))
	}

	for _, s := range got[1] {
		if s.Class != highlight.String {
			t.Errorf("a line inside a raw string was lexed as %d, want a string", s.Class)
		}
	}
}

// A path no grammar answers for draws as plain text rather than as whatever
// chroma guesses a fragment to be.
func TestAnUnknownGrammarYieldsNothing(t *testing.T) {
	t.Parallel()

	if got := highlight.Lines("data/records.unknownext", []string{"anything at all"}); got != nil {
		t.Errorf("an unknown grammar answered %v", got)
	}
}
