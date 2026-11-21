package rate_test

import (
	"fmt"
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

// TestOf pins each score whole. What matters is the order the totals put
// changes in, which the cases are listed in, so a total that moves past its
// neighbor is a reordering and not a reweighting. A hunk nothing could parse
// still counts toward the size and the score says so; a cosmetic hunk is not
// review work, so it is not counted at all; and the sum is bent onto the scale
// rather than clipped, so substantial changes still rank against each other
// well short of the ceiling, which only a vast change reaches.
func TestOf(t *testing.T) {
	t.Parallel()

	signature := structure.Reading{Kind: structure.KindSignature, Parsed: true}
	gained := structure.Reading{Gained: []structure.Class{structure.ClassNetwork, structure.ClassExec}, Parsed: true}
	third := structure.Reading{Gained: []structure.Class{structure.ClassSQL, structure.ClassExec}, Parsed: true}
	exec, network, sql := structure.ClassExec, structure.ClassNetwork, structure.ClassSQL

	for _, tc := range []struct {
		name     string
		readings []structure.Reading
		want     rate.Score
	}{
		{"a re-indent alone", []structure.Reading{{Change: structure.ChangeLayout}}, rate.Score{}},
		{"one line of a body", body(1), rate.Score{Total: 2, Hunks: 1, Parsed: 1}},
		{"nothing parsed", []structure.Reading{{}, {}}, rate.Score{Total: 3, Hunks: 2}},
		{"eight hunks of bodies", body(8), rate.Score{Total: 12, Hunks: 8, Parsed: 8}},
		{
			"a symbol deleted",
			[]structure.Reading{{Kind: structure.KindDeleted, Parsed: true}},
			rate.Score{Total: 14, Deleted: 1, Hunks: 1, Parsed: 1},
		},
		{
			"a symbol added",
			[]structure.Reading{{Kind: structure.KindNew, Parsed: true}},
			rate.Score{Total: 19, New: 1, Hunks: 1, Parsed: 1},
		},
		{
			"a body that reached for a shell",
			[]structure.Reading{{Gained: []structure.Class{exec}, Parsed: true}},
			rate.Score{Total: 35, Gained: []structure.Class{exec}, Hunks: 1, Parsed: 1},
		},
		{
			"a signature changed",
			[]structure.Reading{signature},
			rate.Score{Total: 49, Signature: 1, Hunks: 1, Parsed: 1},
		},
		{
			"and two capabilities",
			[]structure.Reading{signature, gained},
			rate.Score{Total: 78, Signature: 1, Gained: []structure.Class{exec, network}, Hunks: 2, Parsed: 2},
		},
		{
			"and a third",
			[]structure.Reading{signature, gained, third},
			rate.Score{Total: 85, Signature: 1, Gained: []structure.Class{exec, network, sql}, Hunks: 3, Parsed: 3},
		},
		{
			"and ten hunks of bodies", append([]structure.Reading{signature, gained, third}, body(10)...),
			rate.Score{Total: 87, Signature: 1, Gained: []structure.Class{exec, network, sql}, Hunks: 13, Parsed: 13},
		},
		{
			"vast", append(body(400), signature),
			rate.Score{Total: rate.Ceiling, Signature: 1, Hunks: 401, Parsed: 401},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := rate.Of(tc.readings)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) || got.Rated() != (tc.want.Parsed > 0) {
				t.Errorf("Of = %+v, want %+v", got, tc.want)
			}
		})
	}

	cosmetic := rate.Of([]structure.Reading{
		{Change: structure.ChangeComment, Parsed: true},
		{Change: structure.ChangeLayout},
		{Parsed: true},
	})
	if fmt.Sprint(cosmetic) != fmt.Sprint(rate.Of(body(1))) {
		t.Errorf("cosmetic hunks were rated: %+v", cosmetic)
	}
}
