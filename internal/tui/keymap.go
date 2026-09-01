package tui

import "charm.land/bubbles/v2/key"

// The keymap is a motion grammar rather than a key per destination. `]` or `[`
// followed by an object names a motion, `n` and `N` repeat it either way, and
// `.` repeats the last change. Two things follow: `n` keeps the meaning it has
// everywhere else, and an object added later costs no key.
//
// Nothing is chorded beyond the two page keys, because ctrl+c, ctrl+d, ctrl+s,
// and ctrl+z belong to the terminal and Meta chords do not survive tmux and ssh
// intact, which makes a chord the one binding that cannot be relied on.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Forward  key.Binding
	Backward key.Binding
	Again    key.Binding
	Reverse  key.Binding
	Repeat   key.Binding
	NextNote key.Binding
	PrevNote key.Binding
	Edit     key.Binding
	Note     key.Binding
	Shell    key.Binding
	Ready    key.Binding
	Draft    key.Binding
	Skip     key.Binding
	Seen     key.Binding
	Search   key.Binding
	Accept   key.Binding
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
		Forward:  key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "go")),
		Backward: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "go back")),
		Again:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "again")),
		Reverse:  key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "back")),
		Repeat:   key.NewBinding(key.WithKeys("."), key.WithHelp(".", "repeat")),
		NextNote: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		PrevNote: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Note:     key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "note")),
		Shell:    key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "shell")),
		Ready:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "ready")),
		Draft:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "draft")),
		Skip:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "skip")),
		Seen:     key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "read")),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Accept:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "accept")),
		Submit:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "submit")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	}
}

// objects are what `]` and `[` accept. The letter is the first letter of the
// thing, and the order is the order they appear in a review.
func objects() [][2]string {
	return [][2]string{
		{"h", "hunk"},
		{"f", "file"},
		{"c", "comment"},
		{"t", "thread"},
		{"u", "unread hunk"},
	}
}

// helpLines is the full help overlay, one row per line, so the footer can stay
// a single line.
func helpLines() [][2]string {
	return [][2]string{
		{"j / k", "move a line"},
		{"ctrl+d / ctrl+u", "move half a page"},
		{"g / G", "top, bottom"},
		{"] / [", "go to the next, previous: h hunk, f file, c comment, t thread, u unread hunk"},
		{"n / N", "repeat that motion forward, backward"},
		{"/", "search; tab in the prompt restricts it to hunks not yet read"},
		{"tab / shift+tab", "next, previous thing wanting a decision"},
		{".", "repeat the last change"},
		{"space", "mark the hunk read, or the whole file from a file line"},
		{"r / d / x", "mark it ready, draft, or skipped"},
		{"e", "edit a comment, or answer an open thread, in $EDITOR"},
		{"E", "edit the comment's local note in $EDITOR"},
		{"!", "run a shell here and attach what it printed to the note"},
		{"S", "submit the review to GitHub, S again to confirm"},
		{"? / esc", "this help, back"},
		{"q", "quit"},
	}
}
