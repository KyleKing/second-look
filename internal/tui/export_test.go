package tui

// Frame returns one rendered screen, so a test can check the layout without a
// terminal.
func (m *Model) Frame() string { return m.render() }
