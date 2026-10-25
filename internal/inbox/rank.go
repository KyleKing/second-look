package inbox

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
)

// Known is what this laptop already knows about a pull request without asking
// GitHub anything: whether there is a review of it here, and what the diff it
// cached was rated. Both come off disk, so ordering eighty rows costs nothing.
type Known struct {
	Judgement
	// Reviewed is a prepared review for it under .second-look or the user
	// config directory, which is the closest thing to "you have started this"
	// that a search result can be matched against.
	Reviewed bool `json:"reviewed,omitempty"`
	// Cost is what an earlier run rated the diff, and Rated whether there was
	// one to read. A pull request nobody has opened has neither.
	Cost  int  `json:"cost,omitempty"`
	Rated bool `json:"rated,omitempty"`
	// Ready, Draft, and Replies are what a review staged here holds, which is
	// how far into it the last sitting got.
	Ready   int `json:"ready,omitempty"`
	Draft   int `json:"draft,omitempty"`
	Replies int `json:"replies,omitempty"`
	// Added and Removed are how many lines the same read counted. They are
	// shown and never sorted on: a row no grammar answered for is one nobody
	// can rank, and ordering it by line count is the signal the rating exists
	// to reject.
	Added   int `json:"added,omitempty"`
	Removed int `json:"removed,omitempty"`
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

// Rank orders a lane the way triage goes rather than the way a feed does.
//
// A draft sinks whatever else it is, since nobody is waiting on it. What you
// have already started comes next, because finishing a review costs less than
// beginning one. Then a priority label, then a row you are the only human asked
// to review, then one touching the area of the pull request you opened last,
// and last the oldest, since a pull request that has waited a week has waited
// longer than one from this morning. The rating and the line count order
// nothing: they are drawn beside the row for a reader to weigh.
func Rank(items []PullRequest, known func(*PullRequest) Known) {
	slices.SortStableFunc(items, func(a, b PullRequest) int {
		ka, kb := known(&a), known(&b)

		return cmp.Or(
			cmp.Compare(sinks(&a), sinks(&b)),
			cmp.Compare(started(ka), started(kb)),
			cmp.Compare(first(ka.Priority), first(kb.Priority)),
			cmp.Compare(first(ka.Alone), first(kb.Alone)),
			cmp.Compare(first(ka.Area), first(kb.Area)),
			opened(&a).Compare(opened(&b)),
		)
	})
}

// first is 0 for a signal that is set, so it sorts ahead.
func first(set bool) int {
	if set {
		return 0
	}

	return 1
}

func opened(p *PullRequest) time.Time {
	if p.Created.IsZero() {
		return p.Updated
	}

	return p.Created
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

// Attach orders every bucket the way the screen does and records on each row
// what this laptop already knows about it, so a queue read through a pipe
// arrives in the order the rows are worth doing.
func Attach(buckets []Bucket, ratings artifact.Ratings, known map[string]Known) {
	at := func(p *PullRequest) Known {
		return known[artifact.RatingKey(p.Repository, p.Number)]
	}

	for i := range buckets {
		Recall(buckets[i].Items, ratings, known)
		Rank(buckets[i].Items, at)

		for j := range buckets[i].Items {
			p := &buckets[i].Items[j]
			p.Known = at(p)
		}
	}
}
