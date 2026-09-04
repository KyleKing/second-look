package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/seen"
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
	return m.narrowed(m.level())
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
	if !m.onlyNew || m.read == nil {
		return h
	}

	was, why := h.skip, "already read"
	if h.why != "" {
		why = h.why + " and what is already read"
	}

	return hider{why: why, skip: func(p string, at int) bool {
		return (was != nil && was(p, at)) || m.read.Has(seen.Hunk(m.diff, p, at))
	}}
}

// structureMsg carries the pass over every hunk. It runs once per diff and the
// answer is kept, so t after the first press is a redraw.
type structureMsg struct {
	cosmetic map[hunkAt]bool
	shape    shape
	score    rate.Score
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
			cosmetic: out, shape: readShape(readings, at, made), score: rate.Of(readings),
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
