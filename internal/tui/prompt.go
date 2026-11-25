package tui

import (
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
