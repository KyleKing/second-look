package tui

import (
	"context"
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
// touches, the symbols the structural pass named, and the people on the pull
// request, then whoever has been on the repository's recent ones. Nothing here
// needs an index, and what does (every symbol in the repository rather than the
// ones in the diff) waits on codeintel.
func (m *Model) suggestWord() {
	line, col := m.cursorLine()

	stem := trailingWord(line[:col])
	if stem == "" {
		m.say("nothing to complete here", false)

		return
	}

	want := m.candidates(stem)
	if len(want) == 0 {
		m.say(m.nothingWord(stem), false)

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

func (m *Model) nothingWord(stem string) string {
	if m.rostering && strings.HasPrefix(stem, "@") {
		return "still reading who is active on this repository"
	}

	return "nothing in this review starts with " + stem
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
		return prefixed(login, "@", m.logins(), m.recent)
	}

	return prefixed(stem, "", append(m.paths(), m.symbols()...))
}

// prefixed is what in each group starts with the stem, each group ranked above
// the ones after it.
func prefixed(stem, lead string, groups ...[]string) []string {
	var out []string //nolint:prealloc // how many match is known only once each group is filtered

	for _, group := range groups {
		found := []string{}

		for _, want := range group {
			if strings.HasPrefix(want, stem) && want != stem && !slices.Contains(out, lead+want) {
				found = append(found, lead+want)
			}
		}

		sort.Strings(found)
		out = append(out, slices.Compact(found)...)
	}

	return out
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

	for _, syms := range m.shape.touched {
		for i := range syms {
			out = append(out, syms[i].Ident)
		}
	}

	return out
}

// logins is everyone on this pull request, who a comment is likely to name.
func (m *Model) logins() []string {
	out := slices.Concat([]string{m.about.Author}, m.involved)

	for i := range m.threads {
		for _, n := range m.threads[i].Notes {
			out = append(out, n.Author)
		}
	}

	for i := range m.about.Comments {
		out = append(out, m.about.Comments[i].Author)
	}

	return out
}

// Roster reads who an @ can name beyond what the pull request already shows:
// the people on it, and the people on the repository's recent pull requests.
type Roster func(ctx context.Context) (involved, recent []string, err error)

// WithRoster lets @ complete people who have not spoken on the pull request.
func WithRoster(r Roster) Option {
	return func(m *Model) { m.roster = r }
}

type rosterMsg struct {
	involved, recent []string
	err              error
}

func (m *Model) askRoster() tea.Cmd {
	if m.roster == nil || m.rostering || m.rostered {
		return nil
	}

	m.rostering = true
	read, ctx := m.roster, m.ctx

	return func() tea.Msg {
		involved, recent, err := read(ctx)

		return rosterMsg{involved: involved, recent: recent, err: err}
	}
}

func (m *Model) applyRoster(msg rosterMsg) {
	m.rostering, m.rostered = false, true
	m.involved, m.recent = msg.involved, msg.recent

	if msg.err != nil {
		m.say(oneLine(msg.err), true)
	}
}
