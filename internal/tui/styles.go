package tui

import (
	"image/color"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/skin"
	"github.com/kyleking/aragonite/tui/theme"

	"github.com/kyleking/second-look/internal/artifact"
)

// tintShare is how much of the accent a flashing row's background holds at each
// step, from the settled row up. The first is kept under the selection band's
// share so a flash never reads as a second selection.
var tintShare = [flashSteps + 1]float64{0, 0.03, 0.055, 0.08, 0.11}

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
	merged   lipgloss.Style
	priority lipgloss.Style
	// placeholder draws the dot standing in for a fact not yet known. It is
	// decoration, so it is the one queue face the contrast test does not measure.
	placeholder lipgloss.Style
	// band is the background behind the row under the cursor, and nothing else:
	// a face drawn over it supplies the foreground.
	band lipgloss.Style
	// row is the face of the whole row under the cursor, and span the lighter
	// one of the rows an open range covers. Both stay distinguishable where
	// color cannot be drawn, because a tint has nothing to show there.
	row, span lipgloss.Style
	// panel is the background of the inspector and the footer beneath it.
	panel lipgloss.Style
	// hintKey and hintWord are the footer's two faces, drawn on the panel.
	hintKey  lipgloss.Style
	hintWord lipgloss.Style
	// rungs are the faces a queue row is drawn in, each as a pair: dim on the
	// frame's background and lit on the band.
	rungs ladder
	// tint is a row's background at each step of its flash, indexed by Row.Flash.
	tint [flashSteps + 1]lipgloss.Style
	// skin is what the queue's frame is drawn with.
	skin  skin.Skin
	lanes laneFaces
	// Card faces, until aragonite's skin.Edge and skin.Card carry this contrast.
	edge      lipgloss.Style
	agentEdge lipgloss.Style
	fill      color.Color
	severity  map[string]lipgloss.Style
}

// loadStyles is the one palette this process draws with: Ink, whatever the
// terminal's background.
//
//nolint:gochecknoglobals // a process-wide palette, built once
var loadStyles = sync.OnceValue(newStyles)

func newStyles() styles {
	p := theme.Ink()
	sk := skin.New(p, accent(p))
	base := lipgloss.NewStyle()

	// A full Mantle strip reads as a hole in the page rather than a part of it,
	// so the inspector and footer sit on a surface only a step under the base.
	sk.Panel = base.Background(theme.Mix(p.Mantle, p.Base, panelShare))

	var tint [flashSteps + 1]lipgloss.Style
	for step := 1; step <= flashSteps; step++ {
		tint[step] = base.Background(theme.Mix(p.Base, accent(p), tintShare[step]))
	}

	return styles{
		tint:     tint,
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
		behind: base.Foreground(p.Overlay0),
		number: base.Foreground(p.Overlay1),
		// The cursor is drawn as a solid chip rather than sk.Cursor's bold text,
		// since a single lavender character reads the same as the rail and the
		// title's own accent and is lost in a screen already full of them.
		cursor:      base.Background(accent(p)).Foreground(p.Base).Bold(true),
		selected:    sk.Accent,
		rail:        sk.Accent,
		body:        sk.Body,
		note:        sk.Muted.Italic(true),
		footer:      sk.Subtitle,
		head:        sk.Heading,
		key:         sk.Key,
		warn:        sk.Warning,
		fail:        sk.Error,
		ok:          sk.Success,
		merged:      base.Foreground(p.Mauve),
		priority:    base.Foreground(p.Peach).Bold(true),
		placeholder: base.Foreground(p.Overlay0),
		band:        bandFace(p),
		row:         bandFace(p),
		span:        rangeFace(p, sk),
		panel:       sk.Panel,
		hintKey:     base.Foreground(accent(p)),
		hintWord:    base.Foreground(p.Subtext0),
		rungs:       newLadder(p, accent(p)),
		skin:        sk,
		lanes:       newLaneFaces(p),
		edge:        base.Foreground(p.Overlay2),
		agentEdge:   base.Foreground(p.Mauve),
		fill:        p.Surface0,
		severity: map[string]lipgloss.Style{
			"blocker": base.Foreground(p.Red).Bold(true),
			"major":   base.Foreground(p.Peach).Bold(true),
			"minor":   base.Foreground(p.Yellow),
			"nit":     base.Foreground(p.Teal),
			question:  base.Foreground(p.Sky),
		},
	}
}

// bandLift is how far the band's lightness rises toward middle grey, and
// bandSat is the share of the accent's saturation it keeps.
const (
	bandLift = 0.12
	bandSat  = 0.6
)

// bandDepth is how far the cursor's band carries the accent's hue: more than
// the skin's, which on a dark palette reads as no band at all. Tinting rather
// than mixing keeps the lightness down so text on it stays readable.
var bandDepth = theme.Depth{Lift: bandLift, Sat: bandSat}

func bandFace(p theme.Palette) lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.Tint(p.Base, p.Lavender, bandDepth))
}

// spanShare is how much of the accent a range's rows hold, quieter than the
// cursor's band.
const spanShare = 0.08

// panelShare is how far the footer block's surface leans toward Mantle: halfway,
// which keeps it one quiet step under the page rather than a hole in it.
const panelShare = 0.5

func rangeFace(p theme.Palette, sk skin.Skin) lipgloss.Style {
	if sk.Band.GetReverse() {
		return lipgloss.NewStyle().Underline(true)
	}

	return lipgloss.NewStyle().Background(theme.Mix(p.Base, p.Lavender, spanShare))
}

// monochrome swaps the row faces for ones a terminal with no color still draws.
func (s *styles) monochrome() {
	s.row = lipgloss.NewStyle().Reverse(true)
	s.span = lipgloss.NewStyle().Underline(true)
	s.rungs.waiting = rung{dim: s.rungs.waiting.dim.Faint(true), lit: s.rungs.waiting.lit.Faint(true)}
	s.lanes.quietLabel = s.lanes.quietLabel.Faint(true)
	s.lanes.quietCount = s.lanes.quietCount.Faint(true)
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

// rung is one face of a queue row in its two states. Selection puts a band
// behind the row that Overlay2 and dimmer cannot be read on, so every face has
// a variant one step brighter for the row under the cursor.
type rung struct{ dim, lit lipgloss.Style }

// ladder is every face a queue row draws text in.
type ladder struct {
	title, waiting, cell, age, mark, add, remove            rung
	good, fresh, warn, bad, muted, merged, priority, unread rung
}

func newLadder(p theme.Palette, accent color.Color) ladder {
	base := lipgloss.NewStyle()
	fg := func(dim, lit color.Color) rung {
		return rung{dim: base.Foreground(dim), lit: base.Foreground(lit)}
	}
	bold := func(r rung) rung { return rung{dim: r.dim.Bold(true), lit: r.lit.Bold(true)} }

	return ladder{
		title:    rung{dim: base.Foreground(p.Text), lit: base.Foreground(p.Text).Bold(true)},
		waiting:  fg(p.Overlay1, p.Subtext0),
		cell:     fg(p.Subtext0, p.Text),
		age:      fg(p.Overlay1, p.Subtext0),
		mark:     fg(p.Overlay1, p.Subtext0),
		add:      fg(p.Green, p.Green),
		remove:   fg(p.Red, p.Red),
		good:     fg(p.Green, p.Green),
		fresh:    fg(p.Sapphire, p.Sapphire),
		warn:     fg(p.Yellow, p.Yellow),
		bad:      fg(p.Red, p.Red),
		muted:    fg(p.Overlay1, p.Subtext0),
		merged:   fg(p.Mauve, p.Mauve),
		priority: bold(fg(p.Peach, p.Peach)),
		unread:   bold(fg(accent, accent)),
	}
}

// all names every rung, for the contrast test to walk.
func (l *ladder) all() map[string]*rung {
	return map[string]*rung{
		"title": &l.title, "waiting": &l.waiting, "cell": &l.cell, "age": &l.age,
		"mark": &l.mark, "add": &l.add, "remove": &l.remove, "good": &l.good,
		"fresh": &l.fresh, "warn": &l.warn, "bad": &l.bad, "muted": &l.muted, "merged": &l.merged,
		"priority": &l.priority, "unread mark": &l.unread,
	}
}

// laneFaces are the faces of a lane heading: a bold label for lanes that need
// action and a quiet one for those that wait, each with its count.
type laneFaces struct {
	label, count, quietLabel, quietCount lipgloss.Style
}

func newLaneFaces(p theme.Palette) laneFaces {
	base := lipgloss.NewStyle()

	return laneFaces{
		label:      base.Foreground(p.Text).Bold(true),
		count:      base.Foreground(p.Subtext0),
		quietLabel: base.Foreground(p.Overlay1),
		quietCount: base.Foreground(p.Subtext0),
	}
}
