package tui

// Frame returns one rendered screen, so a test can check the layout without a
// terminal.
func (m *Model) Frame() string { return m.render() }

// Failure is the submit that did not post, which Run leaves through.
func (m *Model) Failure() error { return m.failure }
