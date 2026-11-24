package tui

import (
	"image/color"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/highlight"
)

// brush is a style reduced to the escapes it opens and closes a run with, so
// drawing a run is two concatenations rather than a lipgloss Render. Render
// reads a style's every property on each call, which on the rich renderer was
// half of a frame: a line is drawn as a dozen runs, each its own Render.
//
// A style only reduces when what it does to text is wrap it, which brushOf
// checks by rendering two probes rather than by reading properties: a width,
// a border, or a separate face for spaces each draw the probes differently.
// One that does not reduce keeps rendering through lipgloss.
type brush struct {
	open, close string
	ok          bool
}

// brushOf reduces s to a brush, or answers one that is not ok.
func brushOf(s lipgloss.Style) brush {
	const probe, spaced = "x", "x x"

	one := s.Render(probe)

	at := strings.Index(one, probe)
	if at < 0 || strings.Count(one, probe) != 1 {
		return brush{}
	}

	b := brush{open: one[:at], close: one[at+len(probe):], ok: true}
	if s.Render(spaced) != b.open+spaced+b.close {
		return brush{}
	}

	return b
}

// paint draws text as s would. Text carrying a tab or a line break goes through
// lipgloss, which rewrites both.
func (b brush) paint(s lipgloss.Style, text string) string {
	if !b.ok || strings.ContainsAny(text, "\t\n\r") {
		return s.Render(text)
	}

	return b.open + text + b.close
}

// brushSlot is which of the rich renderer's faces a brush was made from. The
// slot and the band behind it are all a face depends on once the screen's
// styles are fixed, which they are for a Model's life.
type brushSlot uint8

const (
	slotCode   brushSlot = iota // a grammar class's face
	slotBehind                  // the face of a hunk already read
	slotGone                    // a merged line's struck-through removal
	slotGutter                  // a line number, loud or quiet
	slotSide                    // a side's mark or sign
	slotPad                     // the band alone, behind spaces
)

// brushKey names one face the rich renderer draws with. Variant tells apart
// faces sharing a slot: the class for code, the line kind for a side, and
// whether a gutter is the quiet one.
type brushKey struct {
	bg      color.Color
	slot    brushSlot
	variant uint8
	class   highlight.Class
	banded  bool
}

// brushes is the screen's brushes by the face they were made from. Rendering
// runs on the Update goroutine, but a lock keeps a render off it from racing.
type brushes struct {
	mu   sync.Mutex
	made map[brushKey]brush
}

func newBrushes() *brushes {
	return &brushes{made: map[brushKey]brush{}}
}

// paint draws text with the brush k names, made from s the first time.
func (c *brushes) paint(k brushKey, s lipgloss.Style, text string) string {
	if c == nil {
		return s.Render(text)
	}

	c.mu.Lock()

	b, ok := c.made[k]
	if !ok {
		b = brushOf(s)
		c.made[k] = b
	}

	c.mu.Unlock()

	return b.paint(s, text)
}
