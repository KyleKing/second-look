package tui

import (
	"image/color"

	"github.com/kyleking/aragonite/tui/theme"
)

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

// DiffColors is everything a change is drawn out of: the frame the bands sit
// on, the color the code is written in, and the four backgrounds themselves at
// the depths a terminal of the given kind gets. A test measures them against
// each other, which is the only way to know a band says anything.
func DiffColors(p theme.Palette, millions bool) map[string]color.Color {
	band, mark := depths(millions)

	return map[string]color.Color{
		"page":         p.Base,
		"text":         p.Text,
		"added band":   blend(p.Base, p.Green, band),
		"removed band": blend(p.Base, p.Red, band),
		"added mark":   blend(p.Base, p.Green, mark),
		"removed mark": blend(p.Base, p.Red, mark),
	}
}

// Reload delivers the watcher's message without waiting on its timer, so a test
// can pin what a write from an agent does to the screen.
func (m *Model) Reload(path string) {
	at, _ := stampOf(path)
	m.reloaded(reloadMsg{at: at})
}

// SetRestage supplies the restager after construction, which is what a test
// needs when the answer has to name the model's own review.
func (m *Model) SetRestage(r Restager) { m.restage = r }

// SawHead is the watcher having found a head that moved, so a test can reach
// the restage without waiting out the check.
func (m *Model) SawHead(sha string) { m.newHead = sha }
