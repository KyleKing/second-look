package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/kyleking/aragonite/tui/keyhint"
)

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
	refreshKey  = "ctrl+r"
	spaceKey    = "space"
)

type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	HalfUp       key.Binding
	PeekUp       key.Binding
	PeekDown     key.Binding
	HalfDown     key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Forward      key.Binding
	Backward     key.Binding
	Again        key.Binding
	Reverse      key.Binding
	Repeat       key.Binding
	NextNote     key.Binding
	PrevNote     key.Binding
	Edit         key.Binding
	Write        key.Binding
	Note         key.Binding
	Shell        key.Binding
	Checkout     key.Binding
	State        key.Binding
	Ready        key.Binding
	Draft        key.Binding
	Skip         key.Binding
	Todo         key.Binding
	Dispatch     key.Binding
	Threads      key.Binding
	Trouble      key.Binding
	Advisories   key.Binding
	Hover        key.Binding
	Restage      key.Binding
	Seen         key.Binding
	Search       key.Binding
	List         key.Binding
	Renderer     key.Binding
	Look         key.Binding
	Order        key.Binding
	Fold         key.Binding
	Zed          key.Binding
	Structure    key.Binding
	OnlyNew      key.Binding
	Round        key.Binding
	Suggest      key.Binding
	Range        key.Binding
	More         key.Binding
	Less         key.Binding
	Accept       key.Binding
	Send         key.Binding
	Submit       key.Binding
	Open         key.Binding
	Merge        key.Binding
	DeleteBranch key.Binding
	React        key.Binding
	About        key.Binding
	Help         key.Binding
	// EndPane is the one key a pane does not get. It is the chord that ends a
	// program which will not leave on its own, chosen because the terminal
	// convention already says hard quit with it.
	EndPane key.Binding
	// Back leaves whatever has the keyboard without leaving the screen. It is
	// esc alone: q shares Quit's binding, and a prompt that reads q as a cancel
	// cannot be typed a word containing one.
	Back key.Binding
	Quit key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:           key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "line")),
		Down:         key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "line")),
		HalfUp:       key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u/d", "half page")),
		PeekUp:       key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y/e", "peek")),
		PeekDown:     key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+y/e", "peek")),
		HalfDown:     key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+u/d", "half page")),
		Top:          key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("gg/G", "top, bottom")),
		Bottom:       key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("gg/G", "top, bottom")),
		Forward:      key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "go")),
		Backward:     key.NewBinding(key.WithKeys("["), key.WithHelp("[", "go back")),
		Again:        key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "again")),
		Reverse:      key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "back")),
		Repeat:       key.NewBinding(key.WithKeys("."), key.WithHelp(".", "repeat")),
		NextNote:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		PrevNote:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
		Edit:         key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Write:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Note:         key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "note")),
		Shell:        key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "shell")),
		Checkout:     key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "check out")),
		State:        key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "state")),
		Ready:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "ready")),
		Draft:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "draft")),
		Skip:         key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "skip")),
		Todo:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "hand back")),
		Dispatch:     key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "dispatch todo")),
		Seen:         key.NewBinding(key.WithKeys(spaceKey), key.WithHelp(spaceKey, "read")),
		Search:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		List:         key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comments")),
		Renderer:     key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "renderer")),
		Look:         key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "toggle one of them")),
		Threads:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "conversations")),
		Trouble:      key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "trouble")),
		Advisories:   key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "ask about a lockfile")),
		Hover:        key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "what is this")),
		Restage:      key.NewBinding(key.WithKeys(refreshKey), key.WithHelp(refreshKey, "restage")),
		Order:        key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "order")),
		Fold:         key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "whitespace")),
		Zed:          key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "fold")),
		Structure:    key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "no code changed")),
		OnlyNew:      key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "only what is new")),
		Round:        key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "since a round")),
		Suggest:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "suggest")),
		Range:        key.NewBinding(key.WithKeys("V"), key.WithHelp("V", "range")),
		More:         key.NewBinding(key.WithKeys("+"), key.WithHelp("+", "more context")),
		Less:         key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "less context")),
		Accept:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "accept")),
		Send:         key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "post one")),
		Submit:       key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "submit")),
		Open:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "on GitHub")),
		Merge:        key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "merge")),
		DeleteBranch: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete branch")),
		React:        key.NewBinding(key.WithKeys(","), key.WithHelp(",", "react")),
		About:        key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "context")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		EndPane:      key.NewBinding(key.WithKeys("ctrl+\\"), key.WithHelp("ctrl+\\", "end the pane's program")),
		Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", quitWord)),
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
		{"p", "problem"},
	}
}

// states are what m accepts, and folds what z accepts. Both are shown while the
// chord waits, so the second key never has to be remembered.
func states() [][2]string {
	return [][2]string{{"r", "ready"}, {"d", "draft"}, {"t", "todo"}, {"x", "skip"}}
}

// lookObjects are what u accepts: the questions the renderers answer
// together and that this toggles one at a time.
func lookObjects() [][2]string {
	return [][2]string{
		{"g", "grammar"}, {"s", "side by side"}, {"p", "what the parser saw"}, {"b", "blame"},
	}
}

// goObjects are what g accepts: gg keeps the top it always was, and d and r
// ask the language server where each name on the line is declared and read.
func goObjects() [][2]string {
	return [][2]string{{"g", "top"}, {"d", "where defined"}, {"r", "where used"}}
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

// helpTree is the legend the help key opens, read a page at a time: the root
// page is what a key does, and a key that waits on a second press carries that
// page under it rather than a row spelling the chord out. Those pages are the
// same tables the chords caption with while they wait, so a key added to one
// lands in the other.
//
// Rounds is what the H chord was last told it can pick, empty where the review
// has been read at one head, so its hint opens a page only when there is one
// to show.
func helpTree(rounds [][2]string) []keyhint.Hint {
	return []keyhint.Hint{
		{What: "moving", Head: true},
		{Key: "j/k", What: "a line"},
		{Key: "ctrl+d/u", What: "half a page"},
		{Key: "ctrl+e/y", What: "peek"},
		{Key: "gg/G", What: "the ends"},
		{Key: "]", What: "go to", Kids: asHints(objects())},
		{Key: "[", What: "go back", Kids: asHints(objects())},
		{Key: "n/N", What: "the same, or back"},
		{Key: ".", What: "the last change"},
		{Key: "tab/shift+tab", What: "what wants a decision"},
		{Key: "/", What: "search"},
		{What: "tab in the prompt narrows a search to hunks not yet read"},
		{What: "what is shown", Head: true},
		{Key: "c", What: "the next view"},
		{Key: "t", What: "threads"},
		{Key: "v", What: "the next renderer"},
		{Key: "u", What: "one of them", Kids: asHints(lookObjects())},
		{Key: "z", What: "folds, the frame", Kids: zPage()},
		{Key: "O", What: "the diff's own order"},
		{Key: "w", What: "whitespace"},
		{Key: "W", What: "no code changed"},
		{Key: "U", What: "only what is new"},
		{Key: "H", What: "since a round", Kids: asHints(rounds)},
		{What: "the code", Head: true},
		{Key: "X", What: "trouble"},
		{Key: "K", What: "what is this"},
		{Key: "g", What: "go", Kids: asHints(goObjects())},
		{Key: "L", What: "the lockfile"},
		{What: "marking", Head: true},
		{Key: spaceKey, What: "mark read"},
		{Key: "m", What: "a state", Kids: asHints(states())},
		{What: "writing", Head: true},
		{Key: "a", What: "a comment", Kids: asHints(severities())},
		{Key: "s", What: "a suggestion"},
		{Key: "V", What: "a range"},
		{Key: "+/-", What: "context"},
		{Key: "e", What: "edit, reply, write"},
		{Key: "E", What: "editing a note"},
		{Key: "!", What: "a shell"},
		{Key: "T", What: "todos to an agent"},
		{What: "while writing: ctrl+t swaps in what an agent wrote, ctrl+n completes a word"},
		{What: "conversations", Head: true},
		{Key: ",", What: "a reaction", Kids: asHints(reactObjects())},
		{What: "the same key takes a reaction back; z then a folds a thread"},
		{What: "the request", Head: true},
		{Key: refreshKey, What: "restage"},
		{Key: "C", What: "check out"},
		{Key: "P", What: "post one"},
		{Key: "S", What: "submit", Kids: asHints(events())},
		{Key: "i", What: "what it says about itself"},
		{Key: "o", What: "on GitHub"},
		{Key: "M", What: "merge"},
		{Key: "D", What: "delete the branch"},
		{What: "leaving", Head: true},
		{Key: "?/esc", What: "this, or back"},
		{Key: "ctrl+\\", What: "end a pane"},
		{Key: "q", What: quitWord},
	}
}

// zPage is the z chord's own legend: the keys that put the cursor's line where
// it is wanted in the frame, then the folds, which is the order the two
// questions come up.
func zPage() []keyhint.Hint {
	return append([]keyhint.Hint{
		{What: "the line at", Head: true},
		{Key: "z", What: "the middle"},
		{Key: "t", What: "the top"},
		{Key: "b", What: "the bottom"},
		{What: "fold", Head: true},
	}, asHints(foldObjects())...)
}
