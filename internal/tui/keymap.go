package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
	Top      key.Binding
	Bottom   key.Binding
	NextHunk key.Binding
	PrevHunk key.Binding
	NextFile key.Binding
	PrevFile key.Binding
	NextNote key.Binding
	PrevNote key.Binding
	Edit     key.Binding
	Note     key.Binding
	Shell    key.Binding
	Ready    key.Binding
	Draft    key.Binding
	Skip     key.Binding
	Submit   key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "line")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "line")),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u/d", "half page")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+u/d", "half page")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/G", "top, bottom")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("g/G", "top, bottom")),
		NextHunk: key.NewBinding(key.WithKeys("n"), key.WithHelp("n/p", "hunk")),
		PrevHunk: key.NewBinding(key.WithKeys("p"), key.WithHelp("n/p", "hunk")),
		NextFile: key.NewBinding(key.WithKeys("}", "]"), key.WithHelp("}/{", "file")),
		PrevFile: key.NewBinding(key.WithKeys("{", "["), key.WithHelp("}/{", "file")),
		NextNote: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "comment")),
		PrevNote: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "comment")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Note:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "note")),
		Shell:    key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "shell")),
		Ready:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "ready")),
		Draft:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "draft")),
		Skip:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "skip")),
		Submit:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "submit")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	}
}

// helpLines is the full help overlay, one row per line, so the footer can stay
// a single line.
func helpLines() [][2]string {
	return [][2]string{
		{"j / k", "move a line"},
		{"ctrl+d / ctrl+u", "move half a page"},
		{"g / G", "top, bottom"},
		{"n / p", "next, previous hunk"},
		{"} / {", "next, previous file"},
		{"tab / shift+tab", "next, previous comment"},
		{"e", "edit a comment, or answer an open thread, in $EDITOR"},
		{"N", "edit the comment's local note in $EDITOR"},
		{"!", "run a shell here and attach what it printed to the note"},
		{"r / d / x", "mark it ready, draft, or skipped"},
		{"S", "submit the review to GitHub, S again to confirm"},
		{"? / esc", "this help, back"},
		{"q", "quit"},
	}
}
