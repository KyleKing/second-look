// Package rate scores how much reading a change is likely to need.
//
// Linear's version counts lines, which is the one signal that does not carry:
// one line of infrastructure config is usually harder than twenty lines and
// fifty-five reformatted ones in a component. So size is a tiebreaker here and
// nothing else.
//
// The score is deterministic and advisory. It orders a queue and it decides
// nothing, which is why there is no threshold in this package: a number a
// reader can compare two rows on is the whole product.
package rate

import (
	"context"
	"math"
	"slices"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/structure"
)

// Weights are what each signal is worth. A signature change outweighs anything
// a body can do, because every caller of the symbol is in scope whether or not
// the diff shows one.
const (
	weightSignature = 40
	weightNew       = 12
	weightDeleted   = 8
	weightGained    = 25
	// The hunk count is the tiebreaker. It is small on purpose: it separates
	// two changes carrying the same signals and never outranks one that does not.
	weightHunk = 1
	// Ceiling keeps the number readable at a glance and comparable between
	// pull requests, which a raw sum of an unbounded diff is not.
	Ceiling = 99
	// How fast the sum approaches the ceiling. It is chosen so that
	// the changes a week of reviewing actually holds land across the range
	// rather than bunched at either end: one hunk of a body rates 2, a symbol
	// added 20, a signature changed 49, a signature plus two new capabilities
	// 78, and a refactor carrying all of it 94.
	scale = 60.0
)

// Score is one pull request's rating and what went into it, so a screen can say
// why a row is near the top rather than showing a number nobody can argue with.
type Score struct {
	Total int
	// Signature, New, and Deleted count the hunks whose strongest effect that
	// was. A hunk is counted once, under the strongest thing it did.
	Signature int
	New       int
	Deleted   int
	// Gained is every capability class the change reaches and the base did not,
	// across the whole diff.
	Gained []structure.Class
	// Hunks is what was rated, which is every hunk that changes code: a
	// re-indent is not review work.
	Hunks int
	// Parsed is how many of those a grammar answered for. A score built from
	// none of them is size alone, and a screen showing it should say so.
	Parsed int
}

// Rated reports whether a grammar answered for anything, which is what
// separates a real score from a hunk count.
func (s Score) Rated() bool { return s.Parsed > 0 }

// Of rates a change from the structural pass over its hunks.
func Of(readings []structure.Reading) Score {
	var s Score

	for i := range readings {
		if readings[i].Change.Cosmetic() {
			continue
		}

		s.Hunks++

		if readings[i].Parsed {
			s.Parsed++
		}

		switch readings[i].Kind {
		case structure.KindSignature:
			s.Signature++
		case structure.KindNew:
			s.New++
		case structure.KindDeleted:
			s.Deleted++
		case structure.KindBody:
		}

		for _, c := range readings[i].Gained {
			if !slices.Contains(s.Gained, c) {
				s.Gained = append(s.Gained, c)
			}
		}
	}

	slices.Sort(s.Gained)
	s.Total = total(s)

	return s
}

// Read makes the structural pass over a diff and answers in the order
// seen.Hunks lists its hunks, so one caller can score the change and another
// can say which hunks changed no code. Both sides of every hunk come from the
// patch, so nothing here reads a working copy.
func Read(ctx context.Context, d *diff.Diff) ([]structure.Reading, []seen.Ref, error) {
	refs := seen.Hunks(d)
	hs := make([]structure.Hunk, 0, len(refs))

	for _, r := range refs {
		before, after := d.Sides(r.Path, r.Hunk)
		hs = append(hs, structure.Hunk{Path: r.Path, Before: before, After: after})
	}

	readings, err := structure.ReadAll(ctx, hs)
	if err != nil {
		//nolint:wrapcheck // ReadAll's error already names the fragment
		return nil, nil, err
	}

	return readings, refs, nil
}

// HunkCost is what reading one hunk is worth, on the same weights the whole
// change is scored on. It orders the reading order rather than a queue, so it
// carries no ceiling: what matters is which of two hunks is dearer, and the
// ceiling exists to keep a number comparable between pull requests.
//
// A hunk nothing parsed costs nothing, which sorts it as if it were cheap. That
// is the honest answer: an unparsed hunk is one nobody can rank, and putting it
// first on a guess would be worse than leaving it where the diff had it.
func HunkCost(r structure.Reading) int {
	if !r.Parsed || r.Change.Cosmetic() {
		return 0
	}

	sum := weightHunk + len(r.Gained)*weightGained

	switch r.Kind {
	case structure.KindSignature:
		sum += weightSignature
	case structure.KindNew:
		sum += weightNew
	case structure.KindDeleted:
		sum += weightDeleted
	case structure.KindBody:
	}

	return sum
}

// total is the signals summed and then bent onto the scale a person reads.
//
// A signature change counts once however many hunks carry one, because the
// second one is the same risk again rather than more of it; the hunk count is
// what grows with the diff.
//
// The sum is bent rather than clipped because clipping is what made the number
// useless: 40 for a signature and 25 for each capability class passed 99 on
// anything substantial, so every real change rated 99 and the number ranked
// nothing. The curve is strictly increasing, so two changes that differ at all
// still rate differently, and it reaches the ceiling only in the limit.
func total(s Score) int {
	sum := s.Hunks * weightHunk

	if s.Signature > 0 {
		sum += weightSignature
	}

	if s.New > 0 {
		sum += weightNew
	}

	if s.Deleted > 0 {
		sum += weightDeleted
	}

	sum += len(s.Gained) * weightGained

	return bend(sum)
}

// bend maps a raw sum onto 0..Ceiling, approaching the ceiling without reaching
// it. Nothing rates zero unless it carries no signal at all.
func bend(sum int) int {
	if sum <= 0 {
		return 0
	}

	return min(int(math.Round(Ceiling*(1-math.Exp(-float64(sum)/scale)))), Ceiling)
}
