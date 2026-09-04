package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/skin"
	"github.com/kyleking/aragonite/tui/theme"

	"github.com/kyleking/second-look/internal/artifact"
)

// accent is the one color that says which of these tools you are looking at.
// Lavender is second-look's: it draws the cursor and the rail a comment hangs
// off, which are the two things this screen is steered by.
func accent(p theme.Palette) color.Color { return p.Lavender }

// styles is what the screens draw with: aragonite's shared faces, so a title
// looks like a title in every one of these tools, plus the few this one needs
// that nothing else has a use for. Every state that carries a color also
// carries a glyph, so a 16-color or NO_COLOR terminal loses emphasis rather
// than meaning.
type styles struct {
	// palette is kept so the rich renderer can derive faces the shared skin has
	// no use for: a band behind code is a background, and skin carries none.
	palette  theme.Palette
	title    lipgloss.Style
	subtitle lipgloss.Style
	file     lipgloss.Style
	hunk     lipgloss.Style
	add      lipgloss.Style
	remove   lipgloss.Style
	context  lipgloss.Style
	behind   lipgloss.Style
	number   lipgloss.Style
	cursor   lipgloss.Style
	selected lipgloss.Style
	rail     lipgloss.Style
	body     lipgloss.Style
	note     lipgloss.Style
	footer   lipgloss.Style
	head     lipgloss.Style
	key      lipgloss.Style
	warn     lipgloss.Style
	fail     lipgloss.Style
	ok       lipgloss.Style
	severity map[string]lipgloss.Style
}

func newStyles() styles {
	p := theme.Detect()
	sk := skin.New(p, accent(p))
	base := lipgloss.NewStyle()

	return styles{
		palette:  p,
		title:    sk.Title,
		subtitle: sk.Subtitle,
		file:     sk.Heading,
		hunk:     sk.Muted,
		// The three diff faces are this screen's own: nothing else here draws a
		// change, and green-is-added is the one convention a reader brings.
		add:     base.Foreground(p.Green),
		remove:  base.Foreground(p.Red),
		context: base.Foreground(p.Subtext1),
		// What is already read recedes, which is nightfox's dim_inactive: the
		// eye then finds what is left instead of counting glyphs.
		behind:   base.Foreground(p.Overlay0),
		number:   base.Foreground(p.Overlay0),
		cursor:   sk.Cursor,
		selected: sk.Accent,
		rail:     sk.Accent,
		body:     sk.Body,
		note:     sk.Muted.Italic(true),
		footer:   sk.Subtitle,
		head:     sk.Heading,
		key:      sk.Key,
		warn:     sk.Warning,
		fail:     sk.Error,
		ok:       sk.Success,
		severity: map[string]lipgloss.Style{
			"blocker": base.Foreground(p.Red).Bold(true),
			"major":   base.Foreground(p.Peach).Bold(true),
			"minor":   base.Foreground(p.Yellow),
			"nit":     base.Foreground(p.Teal),
			question:  base.Foreground(p.Sky),
		},
	}
}

func (s styles) forSeverity(name string) lipgloss.Style {
	if style, ok := s.severity[name]; ok {
		return style
	}

	return s.subtitle
}

// statusGlyph distinguishes the comment states without color, since whether a
// comment will post is the one thing that must survive a monochrome terminal.
func statusGlyph(status string) string {
	switch status {
	case artifact.StatusReady:
		return "●"
	case artifact.StatusDraft:
		return "◐"
	case artifact.StatusSkip:
		return "○"
	case artifact.StatusTodo:
		return "◑"
	default:
		return "?"
	}
}
