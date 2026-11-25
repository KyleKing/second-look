package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/vimtext"
)

// WithVim turns the vim layer of the screen's text fields on or off. It is on
// by default: a field opens in insert mode, so someone who does not use vim
// just types, and esc steps to normal mode before a second one cancels. Off,
// the fields keep the same enter, ctrl+j, and esc contract, with esc canceling
// at once.
func WithVim(on bool) Option {
	return func(m *Model) { m.vim = on }
}

// fieldBase is how many options every field is given before its own.
const fieldBase = 3

// newField is a text field drawn in the screen's faces. Every field on the
// screen shares one register, so a line yanked in the comment editor puts in
// the agent's prompt.
func (m *Model) newField(value string, opts ...vimtext.Option) *vimtext.Field {
	all := make([]vimtext.Option, 0, len(opts)+fieldBase)
	all = append(all,
		vimtext.WithVim(m.vim),
		vimtext.WithStyles(vimtext.StylesFor(&m.styles.skin)),
		vimtext.WithRegister(&m.register))

	return vimtext.New(value, append(all, opts...)...)
}

// fieldEnd is how a message left a field: saved with enter, left with esc out
// of normal mode, or neither while the field is still open. Text is what the
// field held when it ended.
type fieldEnd struct {
	text  string
	saved bool
	left  bool
}

// press hands one message to a field and answers how it ended, if it did. A
// field that ends answers with a command whose only message is its
// vimtext.SubmitMsg or vimtext.CancelMsg, so it is run here rather than
// through the program. Waiting for it there would let a key typed straight
// after enter land on a field that takes no more keys, and be lost.
func press(f *vimtext.Field, msg tea.Msg) (tea.Cmd, fieldEnd) {
	cmd := f.Update(msg)
	if cmd == nil || !f.Done() {
		return cmd, fieldEnd{}
	}

	switch end := cmd().(type) {
	case vimtext.SubmitMsg:
		return nil, fieldEnd{text: end.Text, saved: true}
	case vimtext.CancelMsg:
		return nil, fieldEnd{text: end.Text, left: true}
	}

	return nil, fieldEnd{}
}
