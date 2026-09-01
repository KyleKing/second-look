package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/structure"
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

// structureMsg carries the pass over every hunk. It runs once per diff and the
// answer is kept, so t after the first press is a redraw.
type structureMsg struct {
	cosmetic map[hunkAt]bool
	parsed   int
	err      error
}

// readStructure parses every hunk's two sides. It is a command rather than a
// call because each hunk costs a subprocess, and a screen that stops answering
// keys while it reads is a screen that looks broken.
func readStructure(d *diff.Diff) tea.Cmd {
	return func() tea.Msg {
		refs := seen.Hunks(d)
		hs := make([]structure.Hunk, 0, len(refs))

		for _, r := range refs {
			before, after := d.Sides(r.Path, r.Hunk)
			hs = append(hs, structure.Hunk{Path: r.Path, Before: before, After: after})
		}

		readings, err := structure.ReadAll(context.Background(), hs)
		if err != nil {
			return structureMsg{err: err}
		}

		out := make(map[hunkAt]bool, len(readings))
		parsed := 0

		for i := range readings {
			out[hunkAt{refs[i].Path, refs[i].Hunk}] = readings[i].Change.Cosmetic()

			if readings[i].Parsed {
				parsed++
			}
		}

		return structureMsg{cosmetic: out, parsed: parsed}
	}
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
