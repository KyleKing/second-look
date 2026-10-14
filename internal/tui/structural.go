package tui

import (
	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/structure"
)

// shape is what the structural pass found, kept by hunk so completion can name
// the symbols the diff touched.
//
// It is the same pass the review-cost rating already runs, so none of this
// costs a subprocess of its own: it draws on what was read behind the first
// frame.
type shape struct {
	// touched is the symbols each hunk declared, changed, or removed.
	touched map[hunkAt][]structure.Symbol
	// plan is the order the review reads in, worked out from the same pass.
	plan []order.Group
}

// readShape collects what each hunk did from the pass the rating already ran,
// and works the reading order out of it.
func readShape(readings []structure.Reading, refs []hunkAt, made generated.Set) shape {
	out := shape{touched: map[hunkAt][]structure.Symbol{}}

	for i, r := range readings {
		if i >= len(refs) || !r.Parsed || len(r.Symbols) == 0 {
			continue
		}

		out.touched[refs[i]] = r.Symbols
	}

	out.plan = order.Plan(planHunks(readings, refs, made))

	return out
}

// planHunks is every hunk as the ordering reads it: where it is, what it
// declares, what it calls, and what reading it costs.
func planHunks(readings []structure.Reading, refs []hunkAt, made generated.Set) []order.Hunk {
	out := make([]order.Hunk, 0, len(refs))

	for i, at := range refs {
		h := order.Hunk{
			Ref:  order.Ref{Path: at.path, Hunk: at.hunk},
			Dir:  dirOf(at.path),
			Made: made.Match(at.path),
		}

		if i < len(readings) && readings[i].Parsed {
			h.Calls = readings[i].Called
			h.Cost = rate.HunkCost(readings[i])

			h.Declares = readings[i].Declared
		}

		out = append(out, h)
	}

	return out
}
