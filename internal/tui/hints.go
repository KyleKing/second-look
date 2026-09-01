package tui

import (
	"github.com/kyleking/aragonite/tui/keyhint"
)

// hintLine draws a footer's keys: the key bracketed inside the word it does,
// and bracketed in front where the word does not carry it.
func hintLine(s styles, hints [][2]string) string {
	out := make([]keyhint.Hint, 0, len(hints))
	for _, h := range hints {
		out = append(out, keyhint.Hint{Key: h[0], What: h[1]})
	}

	return keyhint.Line(keyhint.Styles{Key: s.key, Text: s.footer}, out)
}
