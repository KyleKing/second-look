package tui

import (
	"strings"

	"github.com/kyleking/aragonite/tui/keyhint"
)

// helpBlock draws the full legend, keys right-aligned in one column.
func helpBlock(s styles, hints [][2]string, width int) []string {
	return keyhint.Help(
		keyhint.Styles{Key: s.key, Text: s.footer, Head: s.head}, asHints(hints), width,
	)
}

// dimBlock draws the legend with the keys that do nothing where the cursor is
// drawn dim. Both passes lay out the same rows at the same width, so a row is
// swapped whole rather than styled in place.
func dimBlock(s styles, rows []hint, width int) []string {
	pairs := make([][2]string, 0, len(rows))
	for _, r := range rows {
		pairs = append(pairs, [2]string{r.key, r.what})
	}

	on := helpBlock(s, pairs, width)
	off := keyhint.Help(
		keyhint.Styles{Key: s.behind, Text: s.behind, Head: s.head}, asHints(pairs), width,
	)

	if len(off) != len(on) {
		return on
	}

	for i, r := range rows {
		if r.off && i < len(on) {
			on[i] = off[i]
		}
	}

	return on
}

// headMark is the key a heading row carries. Every other key is a keystroke, so
// nothing a screen offers can collide with it.
const headMark = "\x00"

// headRow is a legend row naming the group of keys under it.
func headRow(name string) [2]string { return [2]string{headMark, name} }

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
