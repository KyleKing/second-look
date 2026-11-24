package tui

import (
	"testing"

	"github.com/kyleking/aragonite/diff"
)

// mechanicalHunks checks a hunk's line and byte counts before asking
// WhitespaceOnly, and the check must never turn away a hunk WhitespaceOnly
// would have folded.
func TestMechanicalHunks_AgreesWithWhitespaceOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"reformatted", "-a := f(x,y)\n+a := f(x, y)\n", "whitespace only"},
		{"line ending", "-x := 1\r\n+x := 1\n", "whitespace only"},
		{"reordered and respaced", "-b\n-a b\n+ab\n+  b\n", "whitespace only"},
		{"blank lines only", "+\n+  \n", "whitespace only"},
		{"blank lines around a reformat", "-f(a,b)\n+\n+f(a, b)\n+\t\n", "whitespace only"},
		{"gained a character", "-f(a,b)\n+f(a, b);\n", ""},
		{"same length, other text", "-f(a)\n+g(a)\n", ""},
		{"added only", "+x := 1\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := diff.Parse([]byte("diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n@@ -1,2 +1,2 @@\n" + tt.body))
			got := mechanicalHunks(d, nil)[hunkAt{"f.go", 1}]

			if got != tt.want {
				t.Errorf("mechanicalHunks = %q, want %q", got, tt.want)
			}

			if direct := d.WhitespaceOnly("f.go", 1); direct != (tt.want != "") {
				t.Errorf("WhitespaceOnly = %v, which the case's want disagrees with", direct)
			}
		})
	}
}
