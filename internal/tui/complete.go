package tui

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// wordChars is what counts as part of a word being completed. A path and a Go
// identifier are both one word here, which is what lets the same key answer for
// both without a mode.
const wordChars = "/._-@" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// complete answers ctrl+n while writing: the word under the cursor is replaced
// with the first thing in the review that matches it, and pressing again takes
// the next.
//
// The candidates are what is already on this screen: the files the diff
// touches, the symbols the structural pass named, and the people who have
// already said something on the pull request. Nothing here needs an index, and
// what does (every symbol in the repository rather than the ones in the diff)
// waits on codeintel.
func (m *Model) suggestWord() {
	line, col := m.cursorLine()

	stem := trailingWord(line[:col])
	if stem == "" {
		m.say("nothing to complete here", false)

		return
	}

	want := m.candidates(stem)
	if len(want) == 0 {
		m.say("nothing in this review starts with "+stem, false)

		return
	}

	// A stem that is already one of the answers cycles to the next, so the key
	// walks the list rather than sticking on the first match.
	next := want[0]
	if at := slices.Index(want, stem); at >= 0 {
		next = want[(at+1)%len(want)]
	}

	m.replaceWord(len([]rune(stem)), next)
	m.say(next+"  ·  ctrl+n takes the next of "+matchWord(len(want)), false)
}

func matchWord(n int) string {
	if n == 1 {
		return "1 match"
	}

	return strconv.Itoa(n) + " matches"
}

// cursorLine is the line the cursor is on and how far into it, in runes.
func (m *Model) cursorLine() ([]rune, int) {
	lines := strings.Split(m.editing.area.Value(), "\n")

	at := m.editing.area.Line()
	if at < 0 || at >= len(lines) {
		return nil, 0
	}

	runes := []rune(lines[at])

	return runes, min(max(m.editing.area.Column(), 0), len(runes))
}

func trailingWord(before []rune) string {
	at := len(before)
	for at > 0 && strings.ContainsRune(wordChars, before[at-1]) {
		at--
	}

	return string(before[at:])
}

// replaceWord takes back the stem and puts the completion in its place, through
// the textarea's own editing so the cursor and the undo history stay its own.
func (m *Model) replaceWord(stem int, with string) {
	for range stem {
		area, _ := m.editing.area.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m.editing.area = area
	}

	m.editing.area.InsertString(with)
	m.fit()
	m.keep()
}

// candidates is everything on this screen that starts with the stem, in one
// order so the cycle is the same every time.
func (m *Model) candidates(stem string) []string {
	if login, ok := strings.CutPrefix(stem, "@"); ok {
		return prefixed(m.logins(), login, "@")
	}

	return prefixed(append(m.paths(), m.symbols()...), stem, "")
}

func prefixed(from []string, stem, lead string) []string {
	var out []string

	for _, want := range from {
		if strings.HasPrefix(want, stem) && want != stem {
			out = append(out, lead+want)
		}
	}

	sort.Strings(out)

	return slices.Compact(out)
}

// paths is every file the diff touches, by full path and by base name, since a
// comment naming a file usually names the short one.
func (m *Model) paths() []string {
	out := make([]string, 0, len(m.diff.Files)*2)

	for i := range m.diff.Files {
		p := filePath(&m.diff.Files[i])
		out = append(out, p)

		if at := strings.LastIndex(p, "/"); at >= 0 {
			out = append(out, p[at+1:])
		}
	}

	return out
}

// symbols is what the structural pass named, which is every symbol the diff
// declares rather than every symbol the repository has.
func (m *Model) symbols() []string {
	var out []string

	for _, g := range m.shape.plan {
		if g.Symbol {
			out = append(out, g.Name)
		}
	}

	for _, syms := range m.shape.touched {
		for i := range syms {
			out = append(out, syms[i].Ident)
		}
	}

	return out
}

// logins is everyone who has already said something on this pull request, which
// is who a comment is likely to be addressed to.
func (m *Model) logins() []string {
	var out []string

	for i := range m.threads {
		for _, n := range m.threads[i].Notes {
			out = append(out, n.Author)
		}
	}

	return out
}
