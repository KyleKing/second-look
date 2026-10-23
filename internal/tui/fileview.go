package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/threads"
)

// openFile is a whole file at head, opened from a peek and read in the review's
// own screen. The landing flag stays true until the cursor has been put on line.
type openFile struct {
	path    string
	line    int
	landing bool
	err     error
	spans   [][]highlight.Span
}

// openSite opens the file a peek row points at, at that row's line, and keeps
// where it left from.
func (m *Model) openSite(site diag.Site) tea.Cmd {
	if site.Path == "" {
		return nil
	}

	_, held := m.blobs[site.Path]
	if !held && m.blob == nil {
		m.say("no way to read "+site.Path+" from here", false)

		return nil
	}

	m.jumps.push(m.snapshot())

	m.peeked = nil
	m.whole = &openFile{path: site.Path, line: site.Line, landing: true}
	m.view = viewFile
	m.say("", false)
	m.rebuild()

	if held {
		return nil
	}

	return m.peekBlob(site.Path)
}

// leaveFile answers esc in the file view: back one place, or the diff where
// there is none.
func (m *Model) leaveFile() {
	if to, ok := m.jumps.back(m.snapshot()); ok {
		m.restore(to)

		return
	}

	m.view, m.whole = viewDiff, nil
	m.rebuild()
}

// spansIn is the grammar's reading of one line of the open file, lexed with the
// rest of it the first time any of it is drawn.
func (m *Model) spansIn(line int) []highlight.Span {
	if m.whole.spans == nil {
		m.whole.spans = highlight.Lines(m.whole.path, m.blobs[m.whole.path])
	}

	return spanAt(m.whole.spans, line-1)
}

// landOpen puts the cursor on the line the file was opened at, once its lines
// are on the screen.
func (m *Model) landOpen() {
	f := m.whole
	if f == nil || !f.landing {
		return
	}

	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.kind == rowCode && r.line.New == f.line {
			f.landing = false
			m.cursor = i
			m.reveal()

			return
		}
	}
}

// outsideWord says so when the file open is not one the diff changes.
func (m *Model) outsideWord() string {
	if m.view != viewFile || m.whole == nil {
		return ""
	}

	for i := range m.diff.Files {
		if filePath(&m.diff.Files[i]) == m.whole.path {
			return ""
		}
	}

	return "outside the diff"
}

// buildWhole is a file as it reads at head: every line, with the ones this diff
// adds marked in the gutter the way the diff marks them, and whatever the
// review says about a line hung under it.
//
// A line of a hunk keeps the hunk's number, which is what says a comment can
// anchor to it: GitHub takes one only where the diff has the line.
func buildWhole(
	r *artifact.Review, d *diff.Diff, ts []threads.Thread, lay layout, f *openFile, lines []string, loaded bool,
) screen {
	s := screen{numWidth: max(numberWidth(d), len(strconv.Itoa(len(lines))))}
	head := row{kind: rowFile, text: f.path, path: f.path, comment: noComment}

	switch {
	case f.err != nil:
		s.rows = []row{head, {kind: rowHunk, text: f.err.Error(), path: f.path, comment: noComment}}

		return s
	case !loaded:
		s.rows = []row{head, {kind: rowHunk, text: "reading " + f.path + "…", path: f.path, comment: noComment}}

		return s
	}

	kept := map[int]diff.Line{}

	for i := range d.Files {
		if filePath(&d.Files[i]) != f.path {
			continue
		}

		for _, l := range d.Files[i].Lines {
			if l.Kind != diff.KindRemove {
				kept[l.New] = l
			}
		}
	}

	ctx := codeCtx{
		d: d, r: r, ts: ts, byLine: indexComments(r), byThread: indexThreads(ts),
		placed: make([]bool, len(r.Comments)), lay: lay,
	}

	s.rows = append(make([]row, 0, len(lines)+1), head)

	for i, text := range lines {
		ln := diff.Line{Text: text, Kind: diff.KindContext, New: i + 1}
		if l, ok := kept[ln.New]; ok {
			ln.Kind, ln.Hunk = l.Kind, l.Hunk
		}

		s.rows = append(s.rows, row{kind: rowCode, line: ln, path: f.path, comment: noComment, hunk: ln.Hunk})
		s.rows = append(s.rows, s.hanging(f.path, ln, ctx)...)
	}

	return s
}
