package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/seen"
)

// search is the one place the screen takes typed input. It is a motion like any
// other: committing a pattern sets what n repeats, so a search and a jump to
// the next hunk are walked with the same key.
type search struct {
	input textinput.Model
	// unread restricts matches to hunks nobody has marked read, which is the
	// question a second pass actually asks and no other reviewer answers.
	unread bool
	// pattern is what was last committed, kept so the footer can say what n is
	// repeating after the prompt is gone.
	pattern string
}

func newSearch() search {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = ""

	return search{input: in}
}

// begin opens the prompt. The scope carries over from the last search, since a
// reader working through what is left keeps working through what is left.
func (m *Model) begin() tea.Cmd {
	m.search.input.Reset()
	m.searching = true

	return m.search.input.Focus()
}

// typing feeds the prompt. Enter commits, escape abandons, and tab flips the
// scope between the whole diff and what has not been read.
func (m *Model) typing(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Accept):
		m.searching = false
		m.search.input.Blur()
		m.commit(strings.TrimSpace(m.search.input.Value()))

		return m, nil
	case key.Matches(msg, m.keys.Back):
		m.searching = false
		m.search.input.Blur()
		m.say("", false)

		return m, nil
	case key.Matches(msg, m.keys.NextNote):
		m.search.unread = !m.search.unread

		return m, nil
	}

	in, cmd := m.search.input.Update(msg)
	m.search.input = in

	return m, cmd
}

// commit turns a pattern into the motion n repeats, and runs it once.
func (m *Model) commit(pattern string) {
	if pattern == "" {
		m.say("", false)

		return
	}

	m.search.pattern = pattern
	m.repeatable(motion{1, m.searchWhat(), m.matcher(pattern, m.search.unread)})
}

func (m *Model) searchWhat() string {
	if m.search.unread {
		return "unread match for " + m.search.pattern
	}

	return "match for " + m.search.pattern
}

// matcher accepts a row whose text contains the pattern. Matching is
// case-insensitive until the pattern carries an uppercase letter, which is the
// rule every editor uses and the one nobody has to be told.
func (m *Model) matcher(pattern string, unreadOnly bool) func(row) bool {
	fold := pattern == strings.ToLower(pattern)
	want := pattern

	if fold {
		want = strings.ToLower(pattern)
	}

	return func(r row) bool {
		if unreadOnly && !m.inUnreadHunk(r) {
			return false
		}

		text := rowText(r)
		if fold {
			text = strings.ToLower(text)
		}

		return strings.Contains(text, want)
	}
}

// inUnreadHunk reports whether a row belongs to a hunk nobody has read. A row
// outside any hunk -- the review's own body, a comment listed as not in this
// diff -- belongs to no hunk and so is never in scope.
func (m *Model) inUnreadHunk(r row) bool {
	if r.hunk == 0 || m.read == nil {
		return false
	}

	return !m.read.Has(seen.Hunk(m.diff, r.path, r.hunk))
}

// rowText is what a reader sees on a row, which is what a search over the
// screen has to match: the code, the comment prose, the file name alike.
func rowText(r row) string {
	if r.kind == rowCode {
		return r.line.Text
	}

	return r.text
}
