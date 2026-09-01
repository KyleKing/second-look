package inbox

import (
	"cmp"
	"math"
	"slices"
)

// Known is what this laptop already knows about a pull request without asking
// GitHub anything: whether there is a review of it here, and what the diff it
// cached was rated. Both come off disk, so ordering eighty rows costs nothing.
type Known struct {
	// Reviewed is a prepared review for it under .second-look or the user
	// config directory, which is the closest thing to "you have started this"
	// that a search result can be matched against.
	Reviewed bool
	// Cost is what an earlier run rated the diff, and Rated whether there was
	// one to read. A pull request nobody has opened has neither.
	Cost  int
	Rated bool
}

// Rank orders a bucket the way triage goes rather than the way a feed does.
// Recency is right for thirteen rows and arbitrary for eighty.
//
// What you have already started comes first, because finishing a review costs
// less than beginning one. Then the smallest of what has been rated, since a
// cheap review done now is one fewer thing waiting. Then the oldest, because a
// pull request that has waited a week has waited longer than one from this
// morning. A draft sinks whatever else it is: nobody is waiting on it.
//
// Two things a reviewer would want are missing, and both would cost an API call
// per row: how large the diff is where nothing has rated it, and whether you
// are the only human asked. `gh search prs` returns neither.
func Rank(items []PullRequest, known func(*PullRequest) Known) {
	slices.SortStableFunc(items, func(a, b PullRequest) int {
		ka, kb := known(&a), known(&b)

		return cmp.Or(
			cmp.Compare(sinks(&a), sinks(&b)),
			cmp.Compare(started(ka), started(kb)),
			cmp.Compare(size(ka), size(kb)),
			a.Updated.Compare(b.Updated),
		)
	})
}

// sinks is 1 for a draft, which is not waiting on anybody.
func sinks(p *PullRequest) int {
	if p.Draft {
		return 1
	}

	return 0
}

func started(k Known) int {
	if k.Reviewed {
		return 0
	}

	return 1
}

// size sorts a small rated change ahead of a large one, and everything unrated
// behind both, tying with each other so those rows fall through to age rather
// than being ordered by a number nobody has.
func size(k Known) int {
	if !k.Rated {
		return math.MaxInt
	}

	return k.Cost
}
