package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/rate"
)

// errNoParser is what t says where the structural tool is missing. The w level
// still works, because its test is text and text needs nothing installed.
var errNoParser = errors.New("ast-grep is not installed, so only w can hide anything")

// foldLevel is how much of a diff is held back as not worth reading.
type foldLevel int

const (
	foldNone foldLevel = iota
	// The w level: the two sides say the same things once every space and tab
	// is gone.
	foldWhitespace
	// The t level: that, plus what a parser says changed no code, which is a
	// re-wrap across line boundaries and a comment nobody has to re-read.
	foldCosmetic
)

// hunkAt names one hunk across the diff, which is what a structural reading is
// keyed by.
type hunkAt struct {
	path string
	hunk int
}

// goneAt names one run of removed lines by where it started in the file it came
// from. The file that results carries none of them, so no line of it can.
type goneAt struct {
	path string
	old  int
}

// hider is how the frame holds hunks back and what it says about the ones it
// held. A hunk that vanished with no reason given reads as a bug.
type hider struct {
	skip func(path string, hunk int) bool
	why  string
}

// skipper is what the current level hides, holding nothing back when its skip
// is nil. Both levels ask the text test, because a reordering of whole lines is
// something it catches and comparing the two sides byte by byte does not.
func (m *Model) skipper() hider {
	return m.narrowed(m.fileScoped(m.level()))
}

func (m *Model) level() hider {
	switch m.fold {
	case foldWhitespace:
		return hider{skip: m.diff.WhitespaceOnly, why: "whitespace only"}
	case foldCosmetic:
		return hider{why: "no code changed", skip: func(p string, h int) bool {
			return m.diff.WhitespaceOnly(p, h) || m.cosmetic[hunkAt{p, h}]
		}}
	case foldNone:
		return hider{}
	}

	return hider{}
}

// narrowed adds the incremental filter to whatever level is set. It is a
// separate axis rather than a rung on the ladder: what a parser calls cosmetic
// and what this pass has already read are different questions, and a second
// pass over a pull request wants to ask both.
//
// The test is the read mark, which is keyed by what the hunk says rather than
// by the commit it sat on. So a hunk that survived a force-push unchanged stays
// hidden and one that was touched comes back, which is the whole of "what
// changed since I read it".
func (m *Model) narrowed(h hider) hider {
	h = m.compared(h)

	if !m.onlyNew || m.read == nil {
		return h
	}

	was, why := h.skip, "already read"
	if h.why != "" {
		why = h.why + " and what is already read"
	}

	return hider{why: why, skip: func(p string, at int) bool {
		return (was != nil && was(p, at)) || m.read.Has(m.hunkID(p, at))
	}}
}

// compared adds the round a reader picked to compare against, which hides every
// hunk that round already carried. It is the same test the read marks make,
// asked of what a head held rather than of what this pass has read.
func (m *Model) compared(h hider) hider {
	if m.since == nil {
		return h
	}

	was, why := h.skip, "unchanged since "+short(m.sinceSHA)
	if h.why != "" {
		why = h.why + " and unchanged since " + short(m.sinceSHA)
	}

	return hider{why: why, skip: func(p string, at int) bool {
		return (was != nil && was(p, at)) || m.since.Has(m.hunkID(p, at))
	}}
}

// mechanicalHunks is the hunks a reader has nothing to check in: every changed
// line a reindent of its pair, or the two sides identical once every space and
// tab is gone. One of those has to cover the whole hunk, since a fold that
// buries one real edit among the mechanical ones is worse than no fold. A moved
// run stays open, because its ⇄ note and the jump to the other end only exist
// while the hunk is drawn.
func mechanicalHunks(d *diff.Diff, reindented map[diff.LineRef]bool) map[hunkAt]string {
	type tally struct{ n, indent int }

	tallies := map[hunkAt]*tally{}

	for i := range d.Files {
		p := filePath(&d.Files[i])

		for _, l := range d.Files[i].Lines {
			if l.Kind == diff.KindContext || l.Hunk == 0 {
				continue
			}

			at := hunkAt{p, l.Hunk}
			t := tallies[at]

			if t == nil {
				t = &tally{}
				tallies[at] = t
			}

			t.n++

			if reindented[refOf(p, l)] {
				t.indent++
			}
		}
	}

	out := map[hunkAt]string{}

	for at, t := range tallies {
		switch {
		case t.indent == t.n:
			out[at] = "reindent only"
		case d.WhitespaceOnly(at.path, at.hunk):
			out[at] = "whitespace only"
		}
	}

	return out
}

// structureMsg carries the pass over every hunk. It runs once per diff and the
// answer is kept, so t after the first press is a redraw.
type structureMsg struct {
	cosmetic map[hunkAt]bool
	shape    shape
	score    rate.Score
	size     rate.Size
	err      error
}

// readStructure parses every hunk's two sides. It is a command rather than a
// call because each hunk costs a subprocess, and a screen that stops answering
// keys while it reads is a screen that looks broken.
func readStructure(d *diff.Diff, made generated.Set) tea.Cmd {
	return func() tea.Msg {
		readings, refs, err := rate.Read(context.Background(), d)
		if err != nil {
			return structureMsg{err: err}
		}

		out := make(map[hunkAt]bool, len(readings))
		at := make([]hunkAt, len(readings))

		for i := range readings {
			at[i] = hunkAt{refs[i].Path, refs[i].Hunk}
			out[at[i]] = readings[i].Change.Cosmetic()
		}

		return structureMsg{
			cosmetic: out, shape: readShape(readings, at, made),
			score: rate.Of(readings), size: rate.Measure(d, readings, refs),
		}
	}
}

// newWord says the incremental filter is on, since a review showing three hunks
// of a forty-hunk diff has to say why.
func newWord(on bool) string {
	if on {
		return "showing only what is new since the last pass"
	}

	return "showing every hunk again"
}

// foldWord says which way the fold went, since a hunk that vanished with no
// word for it reads as a bug rather than as a filter.
func foldWord(f foldLevel) string {
	switch f {
	case foldWhitespace:
		return "whitespace-only hunks hidden"
	case foldCosmetic:
		return "hunks that change no code hidden"
	case foldNone:
		return "showing every hunk"
	}

	return "showing every hunk"
}
