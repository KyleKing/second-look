package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// asciiWidth answers only where it agrees with ansi.StringWidth, and the
// strings the screen draws most, code under color, are ones it answers.
func TestASCIIWidth_AgreesWithStringWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		s       string
		answers bool
	}{
		{"", true},
		{"func main() {}", true},
		{"\x1b[38;2;166;227;161;48;2;32;48;32mx := 1\x1b[m", true},
		{"\x1b[1;3m \x1b[0m  \x1b[m", true},
		{"tab\there", false},
		{"naïve", false},
		{"\x1b]8;;https://example.com\x07link\x1b]8;;\x07", false},
		{"\x1b[31", false},
		{"\x1b", false},
		{"\x1b[?25l", true},
		{"\x1b[1 q", true},
		{"\x1b[ 0A", false},
	}

	for _, tt := range tests {
		w, ok := asciiWidth(tt.s)
		if ok != tt.answers {
			t.Errorf("asciiWidth(%q) answers %v, want %v", tt.s, ok, tt.answers)
		}

		if ok && w != ansi.StringWidth(tt.s) {
			t.Errorf("asciiWidth(%q) = %d, StringWidth %d", tt.s, w, ansi.StringWidth(tt.s))
		}

		if got := textWidth(tt.s); got != ansi.StringWidth(tt.s) {
			t.Errorf("textWidth(%q) = %d, StringWidth %d", tt.s, got, ansi.StringWidth(tt.s))
		}
	}
}

func FuzzASCIIWidth(f *testing.F) {
	f.Add("\x1b[38;5;2mok\x1b[m")
	f.Add("a\x1b[1 qb")

	f.Fuzz(func(t *testing.T, s string) {
		if w, ok := asciiWidth(s); ok && w != ansi.StringWidth(s) {
			t.Errorf("asciiWidth(%q) = %d, StringWidth %d", s, w, ansi.StringWidth(s))
		}
	})
}
