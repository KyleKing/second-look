package inbox

import (
	"cmp"
	"slices"
)

// Order arranges a bucket's rows in place, the same shape as Rank. Two rows
// equal on every earlier key still need a last one to break the tie, so every
// order here reaches all the way down to repository and number: two runs of
// the same queue must draw the same rows in the same spots.
type Order func(items []PullRequest, known func(*PullRequest) Known)

// Named is one order under the name a screen shows once it is chosen.
type Named struct {
	Name  string
	Order Order
}

// Orders is every order a screen can cycle through, Rank first since it stays
// what a queue opens to.
var Orders = []Named{
	{Name: "triage", Order: Rank},
	{Name: "oldest first", Order: ByAge},
	{Name: "cheapest first", Order: ByCost},
	{Name: "repository", Order: ByRepository},
}

// ByAge orders the oldest update first.
func ByAge(items []PullRequest, _ func(*PullRequest) Known) {
	slices.SortStableFunc(items, func(a, b PullRequest) int {
		return cmp.Or(
			a.Updated.Compare(b.Updated),
			cmp.Compare(a.Repository, b.Repository),
			cmp.Compare(a.Number, b.Number),
		)
	})
}

// ByCost orders the cheapest rated row first, with every unrated row tied
// behind them and broken by age.
func ByCost(items []PullRequest, known func(*PullRequest) Known) {
	slices.SortStableFunc(items, func(a, b PullRequest) int {
		ka, kb := known(&a), known(&b)

		return cmp.Or(
			cmp.Compare(dearness(ka), dearness(kb)),
			a.Updated.Compare(b.Updated),
			cmp.Compare(a.Repository, b.Repository),
			cmp.Compare(a.Number, b.Number),
		)
	})
}

// ByRepository orders by repository, then by number within it.
func ByRepository(items []PullRequest, _ func(*PullRequest) Known) {
	slices.SortStableFunc(items, func(a, b PullRequest) int {
		return cmp.Or(
			cmp.Compare(a.Repository, b.Repository),
			cmp.Compare(a.Number, b.Number),
		)
	})
}
