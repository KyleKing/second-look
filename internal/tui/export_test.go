package tui

// Frame returns one rendered screen, so a test can check the layout without a
// terminal.
func (m *Model) Frame() string { return m.render() }

// Failure is the submit that did not post, which Run leaves through.
func (m *Model) Failure() error { return m.failure }

// CommentUnderCursor is which comment the cursor is inside, or -1.
func (m *Model) CommentUnderCursor() int { return m.current() }

// CursorRow is which row the cursor is on, so a test can check where a motion
// landed rather than inferring it from the frame.
func (m *Model) CursorRow() int { return m.cursor }

// CursorText is what the row under the cursor says.
func (m *Model) CursorText() string { return rowText(m.screen.rows[m.cursor]) }
