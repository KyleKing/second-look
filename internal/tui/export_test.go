package tui

import "image/color"

// Frame returns one rendered screen, so a test can check the layout without a
// terminal.
func (m *Model) Frame() string { return m.render() }

// Failure is the submit that did not post, which Run leaves through.
func (m *Model) Failure() error { return m.failure }

// CommentUnderCursor is which comment the cursor is inside, or -1.
func (m *Model) CommentUnderCursor() int { return m.current() }

// CommentStatus is what a comment is stamped, read from the review the screen
// holds rather than from the file, so a test can tell a refused keystroke from
// one that changed nothing on disk.
func (m *Model) CommentStatus(i int) string { return m.review.Comments[i].Status }

// CursorRow is which row the cursor is on, so a test can check where a motion
// landed rather than inferring it from the frame.
func (m *Model) CursorRow() int { return m.cursor }

// CursorText is what the row under the cursor says.
func (m *Model) CursorText() string { return rowText(m.screen.rows[m.cursor]) }

// SetSender supplies the single-comment poster after construction, which is
// what a test needs when the sender has to see the model's own review.
func (m *Model) SetSender(s Sender) { m.send = s }

// ListFrame returns one rendered list screen, so a test can check the layout
// without a terminal.
func (l *List) ListFrame() string { return l.render() }

// CursorKey is the row the cursor is on, so a test can check where a motion
// landed rather than inferring it from the frame.
func (l *List) CursorKey() string {
	if row := l.current(); row != nil {
		return row.Key
	}

	return ""
}

// WantsCheckout is C, which the caller answers once the screen has closed.
func (m *Model) WantsCheckout() bool { return m.checkout }

// BandColors is the four backgrounds the rich renderer paints a change with, at
// the depths a terminal of the given kind gets, so a test can check the terminal
// can actually tell them apart.
func BandColors(millions bool) map[string]color.Color {
	p := newStyles().palette
	band, mark := depths(millions)

	return map[string]color.Color{
		"added band":   blend(p.Base, p.Green, band),
		"removed band": blend(p.Base, p.Red, band),
		"added mark":   blend(p.Base, p.Green, mark),
		"removed mark": blend(p.Base, p.Red, mark),
	}
}
