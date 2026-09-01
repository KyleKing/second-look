package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/theme"

	"github.com/kyleking/second-look/internal/artifact"
)

// styles is the palette the review screen draws with. Every state that carries
// a color also carries a glyph, so a 16-color or NO_COLOR terminal loses
// emphasis rather than meaning.
type styles struct {
	title    lipgloss.Style
	subtitle lipgloss.Style
	file     lipgloss.Style
	hunk     lipgloss.Style
	add      lipgloss.Style
	remove   lipgloss.Style
	context  lipgloss.Style
	number   lipgloss.Style
	cursor   lipgloss.Style
	rail     lipgloss.Style
	body     lipgloss.Style
	note     lipgloss.Style
	footer   lipgloss.Style
	key      lipgloss.Style
	warn     lipgloss.Style
	fail     lipgloss.Style
	ok       lipgloss.Style
	severity map[string]lipgloss.Style
}

func newStyles() styles {
	p := theme.Detect()
	base := lipgloss.NewStyle()

	return styles{
		title:    base.Foreground(p.Text).Bold(true),
		subtitle: base.Foreground(p.Subtext0),
		file:     base.Foreground(p.Blue).Bold(true),
		hunk:     base.Foreground(p.Overlay1),
		add:      base.Foreground(p.Green),
		remove:   base.Foreground(p.Red),
		context:  base.Foreground(p.Subtext1),
		number:   base.Foreground(p.Overlay0),
		// Reverse is an attribute rather than a color, so where the cursor is
		// survives NO_COLOR and a 16-color terminal.
		cursor: base.Background(p.Surface1).Reverse(true),
		rail:   base.Foreground(p.Lavender),
		body:   base.Foreground(p.Text),
		note:   base.Foreground(p.Overlay1).Italic(true),
		footer: base.Foreground(p.Subtext0),
		key:    base.Foreground(p.Mauve),
		warn:   base.Foreground(p.Yellow),
		fail:   base.Foreground(p.Red).Bold(true),
		ok:     base.Foreground(p.Green),
		severity: map[string]lipgloss.Style{
			"blocker":  base.Foreground(p.Red).Bold(true),
			"major":    base.Foreground(p.Peach).Bold(true),
			"minor":    base.Foreground(p.Yellow),
			"nit":      base.Foreground(p.Teal),
			"question": base.Foreground(p.Sky),
		},
	}
}

func (s styles) forSeverity(name string) lipgloss.Style {
	if style, ok := s.severity[name]; ok {
		return style
	}

	return s.subtitle
}

// statusGlyph distinguishes the three comment states without color, since
// whether a comment will post is the one thing that must survive a monochrome
// terminal.
func statusGlyph(status string) string {
	switch status {
	case artifact.StatusReady:
		return "●"
	case artifact.StatusDraft:
		return "◐"
	case artifact.StatusSkip:
		return "○"
	default:
		return "?"
	}
}
