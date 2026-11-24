package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/highlight"
)

// lexAsyncLines is the size of a file's diff, in lines, past which its grammar
// is read off the input goroutine. Chroma reads Go at roughly 40µs a line, so
// a file under it lexes inside a frame and one over it, a generated file or a
// new module, would hold the first frame that shows it for as long as the
// grammar takes: 170 ms on a 4,000-line file, the whole of opening it.
const lexAsyncLines = 400

// lexedMsg is one file's grammar, read off the input goroutine from d.
type lexedMsg struct {
	diff  *diff.Diff
	path  string
	spans map[diff.LineRef][]highlight.Span
}

// fileNamed is the file of d at path, or nil.
func fileNamed(d *diff.Diff, path string) *diff.File {
	for i := range d.Files {
		if filePath(&d.Files[i]) == path {
			return &d.Files[i]
		}
	}

	return nil
}

// lexLater reports whether a file is left to lexAhead rather than lexed where
// a frame asks for it, and notes that the frame asked.
func (m *Model) lexLater(path string) bool {
	if m.lexing[path] {
		return true
	}

	f := fileNamed(m.diff, path)
	if f == nil || len(f.Lines) <= lexAsyncLines {
		return false
	}

	if m.lexWanted == nil {
		m.lexWanted = map[string]bool{}
	}

	m.lexWanted[path] = true

	return true
}

// lexAhead starts lexing the long files the screen shows or is about to,
// each once: the ones a frame drew plain, and the ones with a row in the
// window or the page below it, since j and ctrl-d land there next. It runs
// after every Update, so a frame waits on no grammar however it got there.
func (m *Model) lexAhead() tea.Cmd {
	if m.lexing == nil || m.view == viewFile || m.building {
		return nil
	}

	want := m.lexWanted
	m.lexWanted = nil

	rows := m.screen.rows
	lo, hi := max(0, m.offset), min(len(rows), m.offset+2*m.viewHeight())

	for i := lo; i < hi; i++ {
		if p := rows[i].path; p != "" && rows[i].kind == rowCode && !want[p] {
			if want == nil {
				want = map[string]bool{}
			}

			want[p] = true
		}
	}

	var cmds []tea.Cmd

	for p := range want {
		if _, done := m.lexed[p]; done || m.lexing[p] {
			continue
		}

		if f := fileNamed(m.diff, p); f == nil || len(f.Lines) <= lexAsyncLines {
			continue
		}

		m.lexing[p] = true
		d, path := m.diff, p
		cmds = append(cmds, func() tea.Msg {
			return lexedMsg{diff: d, path: path, spans: lexFile(d, path)}
		})
	}

	return tea.Batch(cmds...)
}

// applyLexed keeps a file's grammar, unless the screen has moved on to
// another diff since it was asked for.
func (m *Model) applyLexed(msg lexedMsg) {
	if msg.diff != m.diff {
		return
	}

	delete(m.lexing, msg.path)
	m.lexed[msg.path] = msg.spans
}
