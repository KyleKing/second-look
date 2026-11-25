package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/overlay"
)

// legendStyles is the faces a legend page is drawn in, the same set every
// screen shares so a key reads the same wherever it appears.
func legendStyles(s styles) keyhint.Styles {
	return keyhint.Styles{Key: s.key, Text: s.footer, Head: s.head, Off: s.behind}
}

// boxStyles is the chrome a floating box borrows from the screen: the skin's
// edge, the one face every floating frame in these tools is drawn with, so the
// which-key panel, the keys box, and the palette's drawer read as one family.
func boxStyles(s styles) overlay.Styles {
	return overlay.Styles{Frame: s.skin.Edge, Elision: s.note}
}

// boxMargin is the space a box leaves against the screen's right edge.
const boxMargin = 8

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

// keyOf is the key a binding answers as a hint spells it, so a hint read from
// the keymap follows a rebind or a move.
func keyOf(b key.Binding) string { return b.Help().Key }

// chip draws one hint bare, the key and then its word, with the cell between
// them drawn in gap: keyhint leaves that cell unstyled, which shows as a hole
// wherever the hint sits on a fill or band of its own.
func chip(s keyhint.Styles, h keyhint.Hint, gap lipgloss.Style) string {
	switch {
	case h.Off:
		return s.Off.Render(h.Key + " " + h.What)
	case h.What == "":
		return s.Key.Render(h.Key)
	}

	return s.Key.Render(h.Key) + gap.Render(" ") + s.Text.Render(h.What)
}

// promptKeys are the keys a one-line prompt names after its input, drawn the
// way the footer draws its keys.
func promptKeys(s styles, hints [][2]string) string {
	return "  " + bareLine(s, hints)
}

// bareLine is a short run of keys drawn the way the footer draws its keys,
// two cells apart where the footer leaves three.
func bareLine(s styles, hints [][2]string) string {
	return keyhint.Line(keyhint.Styles{Key: s.hintKey, Text: s.hintWord, Bare: true, Gap: "  "}, asHints(hints))
}

// hintLine spells keys as unstyled text for a status message, bracketed the way
// the footer brackets them.
func hintLine(hints [][2]string) string {
	return keyhint.Line(keyhint.Styles{}, asHints(hints))
}

// footerStyles draws the footer's keys bare: the key in the accent and its word
// quieter, three cells from the next.
func footerStyles(s styles) keyhint.Styles {
	return keyhint.Styles{
		Key: s.hintKey, Text: s.hintWord, Off: s.behind, Bare: true, Gap: footerGap,
		Suggest: s.skin.Suggest.Bold(true), Suggesting: true,
	}
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

func hintPriority(k string) int {
	switch k {
	case enterKey, "Pa", "M", "o", "?":
		return shedLast
	case "C", "l", "s", "/":
		return shedFirst
	}

	return shedLater
}
