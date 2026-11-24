package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// A brush draws exactly what Render draws, and a style it cannot reduce to
// two escapes keeps going through Render.
func TestBrush_PaintsAsRender(t *testing.T) {
	t.Parallel()

	base := lipgloss.NewStyle()
	green, wash := lipgloss.Color("#a6e3a1"), lipgloss.Color("#203020")

	tests := []struct {
		name    string
		style   lipgloss.Style
		reduces bool
	}{
		{"no style", base, true},
		{"foreground", base.Foreground(green), true},
		{"over a band", base.Foreground(green).Background(wash).Italic(true), true},
		{"bold and faint", base.Bold(true).Faint(true), true},
		{"padded", base.Foreground(green).PaddingLeft(2), true},
		{"struck through", base.Foreground(green).Strikethrough(true), false},
		{"underlined", base.Underline(true), false},
		{"a width", base.Width(6), false},
		{"a border", base.Border(lipgloss.NormalBorder()), false},
	}

	texts := []string{"", "x", "  indented code := 1", "tab\there", "naïve ✓ ｗｉｄｅ", "line\r"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := brushOf(tt.style)
			if b.ok != tt.reduces {
				t.Errorf("brushOf reduces = %v, want %v", b.ok, tt.reduces)
			}

			for _, text := range texts {
				if got, want := b.paint(tt.style, text), tt.style.Render(text); got != want {
					t.Errorf("paint(%q) = %q, want %q", text, got, want)
				}
			}
		})
	}
}

// The screen's brushes are made once per face and handed back after that,
// and a nil set still draws.
func TestBrushes_KeepOnePerFace(t *testing.T) {
	t.Parallel()

	c := newBrushes()
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#f38ba8"))
	key := brushKey{slot: slotSide, variant: '-'}

	first := c.paint(key, style, "▎")
	if again := c.paint(key, style, "▎"); again != first || len(c.made) != 1 {
		t.Errorf("a second paint made %d brushes and drew %q after %q", len(c.made), again, first)
	}

	var none *brushes
	if got, want := none.paint(key, style, "x"), style.Render("x"); got != want {
		t.Errorf("a nil set drew %q, want %q", got, want)
	}
}
