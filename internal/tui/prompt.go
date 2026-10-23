package tui

import (
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
func newArea() textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetVirtualCursor(true)

	s := area.Styles()
	s.Cursor.Blink = false
	area.SetStyles(s)

	return area
}
