package tui

import (
	"strings"

	"github.com/kyleking/aragonite/tui/keyhint"
)

// legendStyles is the faces a legend page is drawn in, the same set every
// screen shares so a key reads the same wherever it appears.
func legendStyles(s styles) keyhint.Styles {
	return keyhint.Styles{Key: s.key, Text: s.footer, Head: s.head, Off: s.behind}
}

// helpBlock draws one page of a legend: the group names in a margin with the
// keys wrapped beside them.
func helpBlock(s styles, hints [][2]string, width int) []string {
	return keyhint.Page(legendStyles(s), asHints(hints), width)
}

// headMark is the key a heading row carries. Every other key is a keystroke, so
// nothing a screen offers can collide with it.
const headMark = "\x00"

// asHints is the screens' own key/description pairs as keyhint takes them. A
// pair with no key is a line of prose, which is how a legend carries what the
// keys cannot say.
func asHints(hints [][2]string) []keyhint.Hint {
	out := make([]keyhint.Hint, 0, len(hints))

	for _, h := range hints {
		if h[0] == headMark {
			out = append(out, keyhint.Hint{What: h[1], Head: true})

			continue
		}

		out = append(out, keyhint.Hint{Key: h[0], What: h[1]})
	}

	return out
}

// hintLine draws a footer's keys: the key bracketed inside the word it does,
// and bracketed in front where the word does not carry it.
func hintLine(s styles, hints [][2]string) string {
	return keyhint.Line(keyhint.Styles{Key: s.key, Text: s.footer}, asHints(hints))
}

// dimLine renders a footer where some keys do not apply where the cursor is.
// They are drawn dim rather than dropped: a footer whose keys come and go
// teaches nothing about what the screen offers, and a key that has vanished
// reads as a key that does not exist.
//
// A frame too narrow for the whole line drops the dim ones instead, because a
// footer cut off mid-word loses the keys that leave the screen.
func dimLine(s styles, hints []hint, width int) string {
	line := renderHints(s, hints, false)
	if textWidth(line)+indent <= width {
		return line
	}

	return renderHints(s, hints, true)
}

func renderHints(s styles, hints []hint, drop bool) string {
	on := keyhint.Styles{Key: s.key, Text: s.footer}
	off := keyhint.Styles{Key: s.behind, Text: s.behind}

	out := make([]string, 0, len(hints))

	for _, h := range hints {
		if h.off && drop {
			continue
		}

		face := on
		if h.off {
			face = off
		}

		out = append(out, keyhint.One(face, keyhint.Hint{Key: h.key, What: h.what}))
	}

	return strings.Join(out, keyhint.Gap)
}
