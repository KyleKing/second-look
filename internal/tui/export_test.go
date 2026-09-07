package tui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/theme"

	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/structure"
)

// HunkRef names one hunk for a test that drives the structural pass on readings
// it supplies, rather than on an ast-grep run over a real diff.
type HunkRef struct {
	Path string
	Hunk int
}

// SymbolWords is what each hunk's heading says about the symbols it touched, in
// the order the refs were given, and FileWords is the same for a whole file.
func SymbolWords(readings []structure.Reading, refs []HunkRef) []string {
	sh, at := shapeOf(readings, refs)

	out := make([]string, len(at))
	for i := range at {
		out[i] = sh.symbolWord(at[i])
	}

	return out
}

// FileWord is one file's summary of what the whole file did to its symbols.
func FileWord(readings []structure.Reading, refs []HunkRef, path string) string {
	sh, _ := shapeOf(readings, refs)

	return sh.fileWord(path)
}

func shapeOf(readings []structure.Reading, refs []HunkRef) (shape, []hunkAt) {
	at := make([]hunkAt, len(refs))
	for i, r := range refs {
		at[i] = hunkAt{path: r.Path, hunk: r.Hunk}
	}

	return readShape(readings, at, generated.Set{}), at
}

// Frame returns one rendered screen, so a test can check the layout without a
// terminal.
func (m *Model) Frame() string { return m.render() }

// HeadChecked delivers what the head check answered, which Init asks behind the
// first frame and a test has no program loop to run for it.
func (m *Model) HeadChecked(sha string) {
	m.applyHead(headMsg{sha: sha, want: m.review.HeadSHA})
}

// HeadFailed delivers a head check that could not reach the forge.
func (m *Model) HeadFailed(err error) {
	m.applyHead(headMsg{want: m.review.HeadSHA, err: err})
}

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

// Frame is the screen the shell is drawing, so a test can say which of the two
// it has on without a terminal.
func (s *Shell) Frame() string {
	if s.at == modeReview {
		return s.review.render()
	}

	return s.list.render()
}

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

// CursorAnchor is the line of the file the cursor is standing on, so a test can
// name the range it opened rather than counting keystrokes.
func (m *Model) CursorAnchor() int {
	r := m.screen.rows[m.cursor]

	return anchorOf(r.path, r.line).line
}

// Armed is the number the last cursor move gave the settle timer, so a test can
// deliver that timer without waiting one out.
func (l *List) Armed() int { return l.moves }

// Settle delivers the settle timer armed at at, which is what the cursor
// stopping on a row runs.
func (l *List) Settle(at int) tea.Cmd { return l.settled(at) }

// SetRounds supplies the earlier-round reader after construction, which is what
// a test needs when the diff it answers with is built beside the review.
func (m *Model) SetRounds(r Rounds) { m.rounds = r }

// DimKeys is which footer keys do nothing where the cursor is, which is what
// the frame draws dim and a narrow frame drops.
func (m *Model) DimKeys() []string {
	var out []string

	for _, h := range m.hints() {
		if h.off {
			out = append(out, h.key)
		}
	}

	return out
}

// Inert reports a legend key with nothing to act on under the cursor.
func (m *Model) KeyIsInert(key string) bool { return m.inert(key) }

// Relayout lays out rows for the current view from the whole diff, which is
// what a fold, toggle, or order change runs again rather than only for the
// visible window.
func (m *Model) Relayout() { m.rebuild() }

// Restructure runs the structural pass over the whole diff, one subprocess per
// hunk side, and applies its answer as the command landing behind the first
// frame does.
func (m *Model) Restructure() {
	if msg, ok := readStructure(m.diff, m.made)().(structureMsg); ok {
		m.applyStructure(msg)
	}
}
