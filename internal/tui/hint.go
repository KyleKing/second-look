package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Hints are drawn one way everywhere: the key bracketed inside the word it
// does, and bracketed in front where the word does not carry it. So `[c]omments`
// and `[tab] switch`, which is enough of a legend that the footer needs none.
//
// This is a copy of aragonite's tui/keyhint, which is where it belongs and
// where gh-repo-dashboard reads it from. Delete it, and take the import,
// once aragonite releases the version carrying that package.
const hintGap = "  "

// hintLine renders a row of hints for a footer.
func hintLine(s styles, hints [][2]string) string {
	out := make([]string, 0, len(hints))
	for _, h := range hints {
		out = append(out, hint(s, h[0], h[1]))
	}

	return strings.Join(out, hintGap)
}

// hint renders one key and what it does. A single-character key is bracketed
// where it appears in the word, preferring the start of a word, and the bracket
// carries the key's own case, so a shifted binding reads as `[S]ubmit`.
func hint(s styles, key, what string) string {
	at := hintAt(key, what)
	if at < 0 {
		return s.key.Render("["+key+"]") + " " + s.footer.Render(what)
	}

	// The letter matched in the word is not the key's own byte for a shifted
	// binding, so its width is measured rather than assumed.
	_, size := utf8.DecodeRuneInString(what[at:])

	return s.footer.Render(what[:at]) +
		s.key.Render("["+key+"]") +
		s.footer.Render(what[at+size:])
}

// hintAt is where the key sits in the word, or -1 where it does not. A word
// start wins over a letter in the middle, since that is the one a reader finds
// without looking.
func hintAt(key, what string) int {
	first, size := utf8.DecodeRuneInString(key)
	if size != len(key) || what == "" {
		return -1
	}

	want := unicode.ToLower(first)
	inside := -1

	for i, r := range what {
		if unicode.ToLower(r) != want {
			continue
		}

		if i == 0 || what[i-1] == ' ' {
			return i
		}

		if inside < 0 {
			inside = i
		}
	}

	return inside
}
