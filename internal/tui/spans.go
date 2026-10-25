package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/keyhint"
)

// spanFace is how one piece of a row is drawn, so a heading can carry its name
// at full weight and the words that qualify it a step back.
type spanFace byte

const (
	faceBase spanFace = iota
	faceBold
	faceBody
	faceMuted
	faceHint
)

type chunk struct {
	text string
	face spanFace
	key  string
}

func muted(text string) chunk { return chunk{text: text, face: faceMuted} }

func hintSpan(key, what string) chunk { return chunk{text: what, face: faceHint, key: key} }

func (s chunk) plain() string {
	if s.face == faceHint {
		return "[" + s.key + "] " + s.text
	}

	return s.text
}

// spanned is a row drawn in pieces. Its text stays the pieces read end to end,
// which is what search and the cursor's own readout take.
func spanned(r row, spans ...chunk) row {
	var b strings.Builder

	for _, s := range spans {
		b.WriteString(s.plain())
	}

	r.text, r.spans = b.String(), spans

	return r
}

// headed is a name, the muted words that qualify it, and the keys that act on it.
func headed(name chunk, words []string, hints ...chunk) []chunk {
	out := []chunk{name}

	if len(words) > 0 {
		out = append(out, muted("  "+strings.Join(words, " · ")))
	}

	for _, h := range hints {
		out = append(out, muted("  "), h)
	}

	return out
}

func (m *Model) spanFace(f spanFace, base lipgloss.Style, dim bool) lipgloss.Style {
	switch {
	case f == faceBase:
		return base
	case dim:
		return m.styles.behind
	case f == faceBold:
		return m.styles.body.Bold(true)
	case f == faceBody:
		return m.styles.body
	}

	return m.styles.hunk
}

// spanLine draws a row's pieces after its lead, cut to width. A hint that does
// not fit is dropped whole, because half a key reads as a different key.
func (m *Model) spanLine(r row, lead string, base lipgloss.Style, width int, dim bool) string {
	var b strings.Builder

	b.WriteString(base.Render(cut(lead, width)))

	used := textWidth(lead)

	for _, s := range r.spans {
		room := width - used
		if room <= 0 {
			break
		}

		if s.face == faceHint {
			if textWidth(s.plain()) > room {
				break
			}

			b.WriteString(keyhint.One(m.hintFaces(), keyhint.Hint{Key: s.key, What: s.text}))
			used += textWidth(s.plain())

			continue
		}

		piece := cut(s.text, room)
		b.WriteString(m.spanFace(s.face, base, dim).Render(piece))
		used += textWidth(piece)
	}

	if r.kind == rowFile {
		b.WriteString(m.rule(used, width))
	}

	return b.String()
}

func (m *Model) hintFaces() keyhint.Styles {
	return keyhint.Styles{Key: m.styles.key, Text: m.styles.footer}
}

// rule is the line out to the frame's edge after a heading that has spent used cells.
func (m *Model) rule(used, width int) string {
	rest := width - used - 1
	if rest < ruleFloor {
		return ""
	}

	return m.styles.hunk.Render(" " + strings.Repeat("─", rest))
}
