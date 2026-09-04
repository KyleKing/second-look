package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diff"
)

// step is how much context one press adds or takes away. Three lines is a
// signature and a brace, which is usually the thing a hunk cut off.
const step = 3

// blobMsg is a file as it reads after the change.
type blobMsg struct {
	path  string
	lines []string
	err   error
}

// grow answers + and -: the hunk under the cursor is drawn with more of the
// file around it, or less.
//
// A hunk is three lines of context by convention, and the question a review
// turns on is often in the fourth. Growing it here beats leaving the screen for
// an editor, which loses the comments and the read marks and the place.
func (m *Model) grow(by int) tea.Cmd {
	r := m.screen.rows[m.cursor]
	if r.hunk == 0 || r.path == "" {
		m.say("+ and - grow the context around a hunk, and this row is not in one", false)

		return nil
	}

	at := hunkAt{r.path, r.hunk}

	want := max(0, m.around[at]+by)
	if want == m.around[at] {
		m.say("already showing the whole hunk's surroundings", false)

		return nil
	}

	if _, held := m.blobs[r.path]; !held {
		if m.blob == nil {
			m.say("no way to read "+r.path+" from here", false)

			return nil
		}

		m.say("reading "+r.path+"…", false)

		return m.readBlob(r.path)
	}

	was := m.here()
	m.around[at] = want
	m.rebuild()
	m.goTo(was)
	m.say(fmt.Sprintf("%d line(s) of context around this hunk", want), false)

	return nil
}

func (m *Model) readBlob(path string) tea.Cmd {
	ctx, read := m.ctx, m.blob

	return func() tea.Msg {
		lines, err := read(ctx, path)

		return blobMsg{path: path, lines: lines, err: err}
	}
}

// absorbBlob keeps the file and grows the hunk the reader asked about, since the
// press that started the read is the press that wanted the lines.
func (m *Model) absorbBlob(msg blobMsg) {
	if msg.err != nil {
		m.say("reading "+msg.path+": "+msg.err.Error(), true)

		return
	}

	m.blobs[msg.path] = msg.lines
	m.grow(step)
}

// surround is the lines of the file either side of a hunk, as context rows.
// Nothing is drawn where the file was never read or the hunk was never grown.
//
// The rows carry no pre-image number, because the file that resulted is the one
// being read: a line outside the hunk has no number in the diff at all, and
// inventing one would be a number that lies.
func (m *Model) surround(path string, hunk, from, to int) ([]row, []row) {
	n := m.around[hunkAt{path, hunk}]

	lines, ok := m.blobs[path]
	if n == 0 || !ok {
		return nil, nil
	}

	return aroundLines(lines, path, hunk, max(1, from-n), from-1),
		aroundLines(lines, path, hunk, to+1, min(len(lines), to+n))
}

func aroundLines(lines []string, path string, hunk, from, to int) []row {
	var out []row

	for at := from; at <= to; at++ {
		if at < 1 || at > len(lines) {
			continue
		}

		out = append(out, row{
			kind: rowCode, path: path, comment: noComment, hunk: hunk, around: true,
			line: diff.Line{Text: lines[at-1], Kind: diff.KindContext, New: at, Hunk: hunk},
		})
	}

	return out
}

// bounds is the first and last post-image line of each hunk of a file, which is
// what says where the surroundings start.
func bounds(f *diff.File) map[int][2]int {
	out := map[int][2]int{}

	for _, l := range f.Lines {
		if l.New == 0 {
			continue
		}

		at, held := out[l.Hunk]
		if !held {
			out[l.Hunk] = [2]int{l.New, l.New}

			continue
		}

		out[l.Hunk] = [2]int{min(at[0], l.New), max(at[1], l.New)}
	}

	return out
}
