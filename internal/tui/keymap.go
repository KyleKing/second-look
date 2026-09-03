package tui

import "charm.land/bubbles/v2/key"

// The keymap is a motion grammar rather than a key per destination. `]` or `[`
// followed by an object names a motion, `n` and `N` repeat it either way, and
// `.` repeats the last change. Two things follow: `n` keeps the meaning it has
// everywhere else, and an object added later costs no key.
//
// Every chord is a plain letter followed by another, for the same reason: ctrl+c,
// ctrl+d, ctrl+s, and ctrl+z belong to the terminal and Meta chords do not
// survive tmux and ssh intact, so a modifier is the one binding that cannot be
// relied on. `m` then r, d, or x restamps a comment and `z` then a, R, or M
// folds a note, which leaves both irreversible restamps behind a deliberate
// pair of keys rather than under one letter next to the motion keys.
// Leaving is called the same thing on every screen, so the three footers that
// offer it cannot drift.
const (
	quitWord    = "quit"
	commentWord = "comment"
	spaceKey    = "space"
)

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	HalfUp    key.Binding
	PeekUp    key.Binding
	PeekDown  key.Binding
	HalfDown  key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Forward   key.Binding
	Backward  key.Binding
	Again     key.Binding
	Reverse   key.Binding
	Repeat    key.Binding
	NextNote  key.Binding
	PrevNote  key.Binding
	Edit      key.Binding
	Write     key.Binding
	Note      key.Binding
	Shell     key.Binding
	Checkout  key.Binding
	State     key.Binding
	Ready     key.Binding
	Draft     key.Binding
	Skip      key.Binding
	Seen      key.Binding
	Search    key.Binding
	List      key.Binding
	Renderer  key.Binding
	Fold      key.Binding
	Zed       key.Binding
	Structure key.Binding
	Accept    key.Binding
	Send      key.Binding
	Submit    key.Binding
	Open      key.Binding
	Merge     key.Binding
	Help      key.Binding
	// Back leaves whatever has the keyboard without leaving the screen. It is
	// esc alone: q shares Quit's binding, and a prompt that reads q as a cancel
	// cannot be typed a word containing one.
	Back key.Binding
	Quit key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "line")),
		Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "line")),
		HalfUp:    key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u/d", "half page")),
		PeekUp:    key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y/e", "peek")),
		PeekDown:  key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+y/e", "peek")),
		HalfDown:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+u/d", "half page")),
		Top:       key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/G", "top, bottom")),
		Bottom:    key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("g/G", "top, bottom")),
		Forward:   key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "go")),
		Backward:  key.NewBinding(key.WithKeys("["), key.WithHelp("[", "go back")),
		Again:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "again")),
		Reverse:   key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "back")),
		Repeat:    key.NewBinding(key.WithKeys("."), key.WithHelp(".", "repeat")),
		NextNote:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		PrevNote:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Write:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Note:      key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "note")),
		Shell:     key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "shell")),
		Checkout:  key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "check out")),
		State:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "state")),
		Ready:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "ready")),
		Draft:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "draft")),
		Skip:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "skip")),
		Seen:      key.NewBinding(key.WithKeys(spaceKey), key.WithHelp(spaceKey, "read")),
		Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		List:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comments")),
		Renderer:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "renderer")),
		Fold:      key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "whitespace")),
		Zed:       key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "fold")),
		Structure: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "no code changed")),
		Accept:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "accept")),
		Send:      key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "post one")),
		Submit:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "submit")),
		Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "on GitHub")),
		Merge:     key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "merge")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", quitWord)),
	}
}

// objects are what `]` and `[` accept. The letter is the first letter of the
// thing, and the order is the order they appear in a review.
func objects() [][2]string {
	return [][2]string{
		{"h", "hunk"},
		{"d", "directory"},
		{"f", "file"},
		{"c", commentWord},
		{"t", "thread"},
		{"u", "unread hunk"},
	}
}

// states are what m accepts, and folds what z accepts. Both are shown while the
// chord waits, so the second key never has to be remembered.
func states() [][2]string {
	return [][2]string{{"r", "ready"}, {"d", "draft"}, {"x", "skip"}}
}

func foldObjects() [][2]string {
	return [][2]string{{"a", "fold this"}, {"i", "invert all"}, {"R", "open all"}, {"M", "fold all"}}
}

// events are what the second key of the submit chord can say the review is.
// There is no key for "send it as whatever it already says": what a review is
// posted as is the decision the confirmation exists to take, and a second S
// would take it without naming it.
func events() [][2]string {
	return [][2]string{
		{"a", "approve"},
		{"r", "request changes"},
		{"c", commentWord},
	}
}

// helpLines is the full help overlay, one row per line, so the footer can stay
// a single line.
func helpLines() [][2]string {
	return [][2]string{
		{"j / k", "move a line"},
		{"ctrl+d / ctrl+u", "move half a page"},
		{"ctrl+e / ctrl+y", "scroll without moving the cursor; any motion comes back to it"},
		{"g / G", "top, bottom"},
		{"] / [", "go to the next, previous: d directory, f file, h hunk, c comment, t thread, u unread"},
		{"n / N", "repeat that motion forward, backward"},
		{"/", "search; tab in the prompt restricts it to hunks not yet read"},
		{"tab / shift+tab", "next, previous thing wanting a decision"},
		{".", "repeat the last change: space, m r/d/x, a fold"},
		{"c", "the next view: both, the code as it now reads, the comments alone"},
		{"v", "the next renderer: plain, rich, side by side, structural; each has a caveat"},
		{"w", "hide hunks that change nothing but whitespace, and show them again"},
		{"t", "hide hunks that change no code at all, comments and re-wraps included"},
		{spaceKey, "mark the hunk read, or the whole file from a file line"},
		{"m then r / d / x", "mark the comment ready, draft, or skipped"},
		{"z then a / i / R / M", "fold what is here, or all of it; invert; open all; fold to the file names"},
		{"a then b / m / n / t / ?", "write a comment on this line, ranked blocker to question"},
		{"e", "write here: a comment, an answer to a thread, the review's body or note"},
		{"E", "edit the comment's local note, which never posts"},
		{"!", "run a shell here and attach what it printed to the note"},
		{"C", "move the checkout onto this pull request"},
		{"P", "post the comment under the cursor on its own, now"},
		{"S then a / r / c", "submit, approving, requesting changes, or commenting"},
		{"o", "open the pull request on GitHub"},
		{"M", "squash-merge the pull request, M again to confirm"},
		{"? / esc", "this help, back"},
		{"q", quitWord},
	}
}
