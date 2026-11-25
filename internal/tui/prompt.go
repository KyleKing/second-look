package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
)

// newPrompt is an input with nothing drawn before its text. Its cursor holds
// still, because a blink reschedules every 530ms and a test typing into it waits out each one.
func newPrompt() textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = ""

	s := in.Styles()
	s.Cursor.Blink = false
	in.SetStyles(s)

	return in
}

// newArea is the comment editor's text area, its cursor held still as newPrompt's is.
// Enter is the editor's to save with, as it submits every other prompt in the
// family, so a new line is ctrl+j or alt+enter.
func newArea() textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetVirtualCursor(true)
	area.KeyMap.InsertNewline = key.NewBinding(key.WithKeys(newlineKeys...), key.WithHelp(newlineKey, "new line"))

	s := area.Styles()
	s.Cursor.Blink = false
	area.SetStyles(s)

	return area
}

// newlineKey is the key a text field names for a new line, and newlineKeys
// every key that inserts one: ctrl+j, which every terminal sends as a line
// feed, and alt+enter, which jojo's describe already takes.
const newlineKey = "ctrl+j"

//nolint:gochecknoglobals // a constant list, which Go cannot spell as a const
var newlineKeys = []string{newlineKey, "alt+enter"}
