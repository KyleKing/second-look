package tui

import (
	"context"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/vimtext"
)

// wordChars is what counts as part of a word being completed. A path and a Go
// identifier are both one word here, which is what lets the same key answer for
// both without a mode.
const wordChars = "/._-@" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// completeWord answers ctrl+n while writing: the field replaces the word before
// the cursor with the first thing in the review that starts with it, and each
// press takes the next, back round to what was typed.
//
// The candidates are what is already on this screen: the files the diff
// touches, the symbols the structural pass named, and the people on the pull
// request, then whoever has been on the repository's recent ones. Nothing here
// needs an index, and what does (every symbol in the repository rather than the
// ones in the diff) waits on codeintel.
func (m *Model) completeWord(line string, col int) vimtext.Completion {
	stem := trailingWord(line[:col])
	if stem == "" {
		return vimtext.Completion{Start: col}
	}

	return vimtext.Completion{Start: col - len(stem), Items: m.candidates(stem), Note: m.nothingWord(stem)}
}

func (m *Model) nothingWord(stem string) string {
	if m.rostering && strings.HasPrefix(stem, "@") {
		return "still reading who is active on this repository"
	}

	return "nothing in this review starts with " + stem
}

// trailingWord is the word that ends where the cursor is. Every character a
// word is made of is ASCII, so its length in bytes is its length in the line.
func trailingWord(before string) string {
	at := len(before)
	for at > 0 && strings.IndexByte(wordChars, before[at-1]) >= 0 {
		at--
	}

	return before[at:]
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
