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
	"slices"

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

// Read rates a change by making the structural pass itself, for a caller that
// has hunks and no reading of them.
func Read(ctx context.Context, hs []structure.Hunk) (Score, error) {
	readings, err := structure.ReadAll(ctx, hs)
	if err != nil {
		//nolint:wrapcheck // ReadAll's error already names the fragment
		return Score{}, err
	}

	return Of(readings), nil
}

// total sums the signals under the ceiling. A signature change counts once
// however many hunks carry one, because the second one is the same risk again
// rather than more of it; the hunk count is what grows with the diff.
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

	return min(sum, Ceiling)
}
