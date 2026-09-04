package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Focused is the repository every queue is narrowed to, empty for all of them.
// It holds across the tabs and across the handoffs that close the screen.
func (l *List) Focused() string { return l.focused }

// focus narrows every queue to the cursor row's repository.
func (l *List) focus() {
	row := l.current()
	if row == nil {
		return
	}

	if row.Repo == "" {
		l.status, l.failed = "this row belongs to no repository", true

		return
	}

	l.focused = row.Repo
	l.status, l.failed = "focused "+row.Repo, false
	l.rebuild()
}

func (l *List) unfocus() {
	if l.focused == "" {
		return
	}

	l.focused = ""
	l.status, l.failed = "", false
	l.rebuild()
}

func (l *List) focusKey(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, l.list.Focus):
		l.focus()
	case key.Matches(msg, l.list.Unfocus):
		l.unfocus()
	default:
		return false
	}

	return true
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
