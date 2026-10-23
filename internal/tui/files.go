package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// files is the diff narrowed to the files worth reading, as a hider rather
// than a fold: a file it hides keeps its heading and says why, the way a
// whitespace-only hunk does, so nothing ever looks missing without a reason.
type files struct {
	input textinput.Model
	// query is what was committed, kept so the title can say what the diff is
	// narrowed to and the prompt reopens with it.
	query string
	// typing is whether the prompt owns the keyboard.
	typing bool
}

func newFiles() files {
	return files{input: newPrompt()}
}

// fileQuery is the pattern narrowing the diff right now: the prompt's text
// while it is open, since a pattern that only narrows after enter is one
// nobody can see working, and what was committed once it closes.
func (m *Model) fileQuery() string {
	if m.files.typing {
		return strings.TrimSpace(m.files.input.Value())
	}

	return m.files.query
}

// beginFiles opens the prompt with what is already narrowing the diff, so a
// filter is corrected rather than retyped.
func (m *Model) beginFiles() tea.Cmd {
	m.files.typing = true
	m.files.input.SetValue(m.files.query)
	m.files.input.CursorEnd()
	m.say("", false)

	return m.files.input.Focus()
}

// typeFiles feeds the prompt. Enter keeps what is typed, escape puts the
// committed pattern back, and anything else narrows live so the count under
// the prompt answers before it is committed.
func (m *Model) typeFiles(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Accept):
		m.files.typing = false
		m.files.input.Blur()
		m.files.query = strings.TrimSpace(m.files.input.Value())
		m.rebuild()
		m.say(m.fileWord(), false)

		return m, nil
	case key.Matches(msg, m.keys.Back):
		m.files.typing = false
		m.files.input.Blur()
		m.rebuild()

		return m, nil
	}

	in, cmd := m.files.input.Update(msg)
	m.files.input = in
	m.rebuild()

	return m, cmd
}

// fileScoped adds the file pattern to whatever else is hiding hunks. It is a
// separate axis rather than a fold level: which files are worth reading at
// all is a question the whitespace and cosmetic passes do not ask.
func (m *Model) fileScoped(h hider) hider {
	query := m.fileQuery()
	if query == "" {
		return h
	}

	match := fileMatch(query)
	was, why := h.skip, "filtered by "+query
	if h.why != "" {
		why = h.why + " and filtered by " + query
	}

	return hider{why: why, skip: func(p string, at int) bool {
		return (was != nil && was(p, at)) || !match(p)
	}}
}

// fileWord is the pattern the diff is narrowed to, kept on the title line
// because nothing else says why a file is showing only a heading.
func (m *Model) fileWord() string {
	if q := m.fileQuery(); q != "" {
		return "files: " + q
	}

	return ""
}

// filesPrompt is the filter line, which says how many files the pattern
// keeps because a diff that is quiet for the wrong reason is the worst thing
// a filter can do.
func (m *Model) filesPrompt() string {
	head := m.styles.key.Render("f") + m.styles.footer.Render(" ")
	tail := m.styles.footer.Render("  " + m.filesCounted())

	return cut(head+m.files.input.View()+tail, m.width)
}

// filesCounted is how much of the diff's file list the pattern is showing.
func (m *Model) filesCounted() string {
	match := fileMatch(m.fileQuery())
	shown := 0

	for i := range m.diff.Files {
		if match(filePath(&m.diff.Files[i])) {
			shown++
		}
	}

	return fmt.Sprintf("showing %d of %d files", shown, len(m.diff.Files))
}

// fileMatch is the predicate one query gives. A file shows when it matches
// a positive token and none of the negated ones. `*.go internal` is go files
// or anything under internal, `!*.lock` is everything but locks, and
// `*.go !*_test.go` is go files minus their tests.
func fileMatch(query string) func(string) bool {
	var yes, no []string

	for _, tok := range strings.Fields(query) {
		if rest, ok := strings.CutPrefix(tok, "!"); ok {
			no = append(no, rest)
		} else {
			yes = append(yes, tok)
		}
	}

	return func(p string) bool {
		for _, pat := range no {
			if globMatch(pat, p) {
				return false
			}
		}

		for _, pat := range yes {
			if globMatch(pat, p) {
				return true
			}
		}

		return len(yes) == 0
	}
}

// globMatch reports whether pat matches path. A * reaches across directory
// separators the way it does in a .gitignore, a `?` matches one rune, and a
// pattern carrying neither is a plain substring so ".go" and "internal"
// name what they mean without stars.
func globMatch(pat, p string) bool {
	if !strings.ContainsAny(pat, "*?") {
		return strings.Contains(p, pat)
	}

	return glob([]rune(pat), []rune(p))
}

func glob(pat, p []rune) bool {
	for len(pat) > 0 {
		switch pat[0] {
		case '*':
			for i := 0; i <= len(p); i++ {
				if glob(pat[1:], p[i:]) {
					return true
				}
			}

			return false
		case '?':
			if len(p) == 0 {
				return false
			}

			pat, p = pat[1:], p[1:]
		default:
			if len(p) == 0 || pat[0] != p[0] {
				return false
			}

			pat, p = pat[1:], p[1:]
		}
	}

	return len(p) == 0
}
