package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Focused is the repository every queue is narrowed to, empty for all of them.
// It holds across the tabs and across the handoffs that close the screen.
func (l *List) Focused() string { return l.focused }

// FocusNote is what a caller found out about the focused repository, drawn in
// the header beside it. An answer about a repository no longer in focus is
// dropped rather than drawn against the wrong one.
type FocusNote struct {
	Repo string
	Note string
}

// WithFocusNote asks the caller for one line about the repository each time
// focus moves. It returns a command, since answering it here means reading a
// disk cache or running another tool.
func (l *List) WithFocusNote(on func(repo string) tea.Cmd) *List {
	l.onFocus = on

	return l
}

func (l *List) noteFocus() tea.Cmd {
	l.focusNote = ""

	if l.onFocus == nil || l.focused == "" {
		return nil
	}

	return l.onFocus(l.focused)
}

// focus narrows every queue to the cursor row's repository.
func (l *List) focus() tea.Cmd {
	row := l.current()
	if row == nil {
		return nil
	}

	if row.Repo == "" {
		l.status, l.failed = "this row belongs to no repository", true

		return nil
	}

	l.focused = row.Repo
	l.status, l.failed = "focused "+row.Repo, false
	l.rebuild()

	return l.noteFocus()
}

func (l *List) unfocus() {
	if l.focused == "" {
		return
	}

	l.focused, l.focusNote = "", ""
	l.status, l.failed = "", false
	l.rebuild()
}

func (l *List) focusKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, l.list.Focus):
		return l.focus(), true
	case key.Matches(msg, l.list.Unfocus):
		l.unfocus()
	default:
		return nil, false
	}

	return nil, true
}

// keepFocused drops the rows belonging to another repository. A row naming no
// repository stays, since a search that failed carries one and the reason a
// section is empty is the thing a narrowed queue must not hide.
func keepFocused(repo string, sections []Section) []Section {
	if repo == "" {
		return sections
	}

	out := make([]Section, 0, len(sections))

	for i := range sections {
		kept := make([]Row, 0, len(sections[i].Rows))

		for j := range sections[i].Rows {
			if r := &sections[i].Rows[j]; r.Repo == "" || strings.EqualFold(r.Repo, repo) {
				kept = append(kept, *r)
			}
		}

		out = append(out, Section{Name: sections[i].Name, Note: sections[i].Note, Rows: kept})
	}

	return out
}
