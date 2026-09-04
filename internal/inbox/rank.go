package inbox

import (
	"cmp"
	"math"
	"slices"

	"github.com/kyleking/second-look/internal/artifact"
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
	// Added and Removed are how many lines the same read counted. They are
	// shown and never sorted on: a row no grammar answered for is one nobody
	// can rank, and ordering it by line count is the signal the rating exists
	// to reject.
	Added, Removed int
}

// WorthRating is how many rows a bucket needs before its order is worth an API
// read per row. Recency orders a screenful well enough, and a bucket you can
// see all of at once is one you can order yourself; past that the rating is
// what separates the cheap review from the one that costs an afternoon.
const WorthRating = 20

// Recall fills in what earlier runs made of these rows, so a second open of the
// same queue orders itself off disk. A rating is about the diff at one moment,
// so one recorded against another update time is dropped rather than trusted: a
// push since then is exactly the case where the number would mislead.
//
// It answers Asked as well as Known, which is every row an earlier run already
// fetched the diff of, rated or not. Fetching the same unratable diff on every
// open is what that saves.
func Recall(items []PullRequest, ratings artifact.Ratings, known map[string]Known) map[string]bool {
	asked := make(map[string]bool, len(items))

	for i := range items {
		p := &items[i]
		key := artifact.RatingKey(p.Repository, p.Number)

		was, ok := ratings[key]
		if !ok || !was.Updated.Equal(p.Updated) {
			continue
		}

		asked[key] = true

		k := known[key]

		if k.Added == 0 && k.Removed == 0 {
			k.Added, k.Removed = was.Added, was.Removed
		}

		if !k.Rated && was.Rated {
			k.Cost, k.Rated = was.Cost, true
		}

		known[key] = k
	}

	return asked
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
// Size is deliberately not in the order. It is drawn beside the rating so a
// reader can check one against the other, and sorting on it would be the line
// count the rating exists to replace. Whether you are the only human asked is
// the signal still missing, and it costs an API call per row.
func Rank(items []PullRequest, known func(*PullRequest) Known) {
	slices.SortStableFunc(items, func(a, b PullRequest) int {
		ka, kb := known(&a), known(&b)

		return cmp.Or(
			cmp.Compare(sinks(&a), sinks(&b)),
			cmp.Compare(started(ka), started(kb)),
			cmp.Compare(dearness(ka), dearness(kb)),
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

// dearness sorts a cheap rated change ahead of a dear one, and everything
// unrated behind both, tying with each other so those rows fall through to age
// rather than being ordered by a number nobody has.
func dearness(k Known) int {
	if !k.Rated {
		return math.MaxInt
	}

	return k.Cost
}
