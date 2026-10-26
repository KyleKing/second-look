package tui

import (
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/overlay"
)

// legendStyles is the faces a legend page is drawn in, the same set every
// screen shares so a key reads the same wherever it appears.
func legendStyles(s styles) keyhint.Styles {
	return keyhint.Styles{Key: s.key, Text: s.footer, Head: s.head, Off: s.behind}
}

// boxStyles is the chrome a floating box borrows from the screen: the quiet
// faces, since the border is the least important thing it draws.
func boxStyles(s styles) overlay.Styles {
	return overlay.Styles{Frame: s.subtitle, Elision: s.note}
}

// boxBorder is the rows a floating box spends on its top and bottom frame.
const boxBorder = 2

// boxMargin is the space a box leaves against the screen's right edge.
const boxMargin = 8

// pastEnd is a scroll offset the render clamp pins to a page's last row.
const pastEnd = 1 << 30

// wheelStep is the lines one notch of the wheel moves inside a scrolling
// overlay.
const wheelStep = 3

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

// hintLine spells keys as unstyled text for a status message, bracketed the way
// the footer brackets them.
func hintLine(hints [][2]string) string {
	return keyhint.Line(keyhint.Styles{}, asHints(hints))
}

// footerStyles draws the footer's keys bare: the key in the accent and its word
// quieter, three cells from the next.
func footerStyles(s styles) keyhint.Styles {
	return keyhint.Styles{Key: s.hintKey, Text: s.hintWord, Off: s.behind, Bare: true, Gap: footerGap}
}

const footerGap = "   "

// The keys a footer sheds last, in the order a frame runs out of room. Enter,
// approve, merge, GitHub, and help are what a queue is for and never go while
// anything else is drawn.
const (
	shedFirst = iota + 1
	shedLater
	shedLast
)

func hintPriority(key string) int {
	switch key {
	case enterKey, "A", "M", "o", "?":
		return shedLast
	case "C", "m", "l", "s", "/":
		return shedFirst
	}

	return shedLater
}
