package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// filter narrows a queue to the rows worth looking at. A configured inbox runs
// to eighty rows across four sections, which no amount of layout makes scannable,
// and the row you want is nearly always named by a word you already know: the
// repository, the author, a word from the title.
type filter struct {
	input textinput.Model
	// query is what was committed, kept so the rows stay narrowed after the
	// prompt closes and the header can say what it is narrowed to.
	query string
	// typing is whether the prompt owns the keyboard.
	typing bool
}

func newFilter() filter {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = ""

	return filter{input: in}
}

// on reports whether anything is narrowing the queue.
func (f filter) on() bool { return f.query != "" || f.typing }

// beginFilter opens the prompt with what is already narrowing the list, so a
// filter is corrected rather than retyped.
func (l *List) beginFilter() tea.Cmd {
	l.filter.typing = true
	l.filter.input.SetValue(l.filter.query)
	l.filter.input.CursorEnd()
	l.status, l.failed = "", false

	return l.filter.input.Focus()
}

// typeFilter narrows as it is typed, so what the pattern catches is visible
// before it is committed. Enter keeps it, and escape puts the queue back.
func (l *List) typeFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, l.keys.Accept):
		l.filter.typing = false
		l.filter.input.Blur()

		return l, nil
	case key.Matches(msg, l.keys.Back):
		l.clearFilter()

		return l, nil
	}

	in, cmd := l.filter.input.Update(msg)
	l.filter.input = in
	l.filter.query = strings.TrimSpace(in.Value())
	l.rebuild()

	return l, cmd
}

func (l *List) clearFilter() {
	l.filter.typing = false
	l.filter.query = ""
	l.filter.input.Blur()
	l.filter.input.Reset()
	l.rebuild()
	l.status, l.failed = "", false
}

// keeps reports whether a row answers the filter. Matching is case-insensitive
// until the pattern carries an uppercase letter, which is the rule every editor
// uses and the one nobody has to be told.
func (f filter) keeps(section string, r *Row) bool {
	if f.query == "" {
		return true
	}

	fold := f.query == strings.ToLower(f.query)
	hay := strings.Join([]string{section, r.Left, r.Mid, r.Tail, r.Under}, "\x00")

	if fold {
		return strings.Contains(strings.ToLower(hay), f.query)
	}

	return strings.Contains(hay, f.query)
}

// narrow drops the rows the filter does not keep, and the sections left with
// none. An empty section normally keeps its heading, because nothing waiting on
// you is worth seeing; a section holding nothing that matches is noise.
func (f filter) narrow(sections []Section) []Section {
	if f.query == "" {
		return sections
	}

	out := make([]Section, 0, len(sections))

	for i := range sections {
		kept := make([]Row, 0, len(sections[i].Rows))

		for j := range sections[i].Rows {
			if f.keeps(sections[i].Name, &sections[i].Rows[j]) {
				kept = append(kept, sections[i].Rows[j])
			}
		}

		if len(kept) > 0 {
			out = append(out, Section{Name: sections[i].Name, Rows: kept})
		}
	}

	return out
}

// prompt is the filter line, which says what is narrowing the list and how many
// rows are left, because a queue that is quiet for the wrong reason is the worst
// thing a filter can do.
func (l *List) prompt() string {
	head := l.styles.key.Render("/") + l.styles.footer.Render(" ")
	tail := l.styles.footer.Render("  " + l.counted())

	return cut(head+l.filter.input.View()+tail, l.width)
}

// counted is how much of the queue the filter is showing.
func (l *List) counted() string {
	shown, all := 0, 0

	for i := range l.shown {
		shown += len(l.shown[i].Rows)
	}

	for _, s := range l.sections() {
		all += len(s.Rows)
	}

	return fmt.Sprintf("showing %d of %d", shown, all)
}
