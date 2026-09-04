package tui

import (
	"github.com/kyleking/aragonite/tui/keyhint"
)

// helpBlock draws the full legend, keys right-aligned in one column.
func helpBlock(s styles, hints [][2]string, width int) []string {
	return keyhint.Help(
		keyhint.Styles{Key: s.key, Text: s.footer, Head: s.head}, asHints(hints), width,
	)
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
