package rate_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/structure"
)

func body(n int) []structure.Reading {
	out := make([]structure.Reading, 0, n)
	for range n {
		out = append(out, structure.Reading{Parsed: true})
	}

	return out
}

// The point of the rating is the order it puts changes in, so that is what is
// pinned rather than the numbers, which are weights and will move.
func TestOfOrdersChangesByWhatTheyCarry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		readings []structure.Reading
	}{
		{name: "a re-indent alone", readings: []structure.Reading{{Change: structure.ChangeLayout}}},
		{name: "one line of a body", readings: body(1)},
		{name: "eight hunks of bodies", readings: body(8)},
		{name: "a symbol deleted", readings: []structure.Reading{{Kind: structure.KindDeleted, Parsed: true}}},
		{name: "a symbol added", readings: []structure.Reading{{Kind: structure.KindNew, Parsed: true}}},
		{
			name:     "a body that reached for a shell",
			readings: []structure.Reading{{Gained: []structure.Class{structure.ClassExec}, Parsed: true}},
		},
		{
			name:     "a signature changed",
			readings: []structure.Reading{{Kind: structure.KindSignature, Parsed: true}},
		},
		{
			name: "a signature changed and a capability gained",
			readings: []structure.Reading{
				{Kind: structure.KindSignature, Parsed: true},
				{Gained: []structure.Class{structure.ClassExec, structure.ClassNetwork}, Parsed: true},
			},
		},
	}

	last := -1

	for _, c := range cases {
		got := rate.Of(c.readings).Total
		if got <= last {
			t.Errorf("%s rates %d, which is not above the case before it (%d)", c.name, got, last)
		}

		last = got
	}
}

// A hunk nothing could parse still counts toward the size, and the score says
// so, because a number that cannot tell a signature change from a comment
// should not be read as one that can.
func TestOfSaysWhenNothingParsed(t *testing.T) {
	t.Parallel()

	s := rate.Of([]structure.Reading{{}, {}})

	if s.Rated() {
		t.Error("a score with no parsed hunk claims to be rated")
	}

	if s.Hunks != 2 {
		t.Errorf("hunks = %d, want 2", s.Hunks)
	}

	if s.Total == 0 {
		t.Error("two changed hunks rate zero")
	}
}

// A cosmetic hunk is not review work, so it is not in the count the size term
// reads: hiding it with w or t and rating it have to agree.
func TestOfSkipsCosmeticHunks(t *testing.T) {
	t.Parallel()

	s := rate.Of([]structure.Reading{
		{Change: structure.ChangeComment, Parsed: true},
		{Change: structure.ChangeLayout},
		{Parsed: true},
	})

	if s.Hunks != 1 {
		t.Errorf("hunks = %d, want 1", s.Hunks)
	}
}

// The ceiling is what keeps two large changes comparable at a glance.
func TestOfStopsAtTheCeiling(t *testing.T) {
	t.Parallel()

	huge := append(body(400), structure.Reading{Kind: structure.KindSignature, Parsed: true})

	if got := rate.Of(huge).Total; got != rate.Ceiling {
		t.Errorf("total = %d, want the ceiling %d", got, rate.Ceiling)
	}
}
