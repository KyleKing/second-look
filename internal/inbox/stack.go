package inbox

import (
	"cmp"
	"slices"

	"github.com/kyleking/second-look/internal/stack"
)

// Stack is where a row sits in a chain of open pull requests stacked on one
// another. The zero value is a row standing alone.
type Stack struct {
	// ID is the key of the bottom of the chain, shared by every row in it.
	ID string `json:"id"`
	// Position counts from the bottom, which is 1, and Size is the chain's length.
	Position int `json:"position"`
	Size     int `json:"size"`
}

// Above reports a row with a pull request beneath it, which is read after that one.
func (s Stack) Above() bool { return s.Position > 1 }

// stacked records the Stack of every open row that sits in a chain and says
// which lane each of those rows is drawn in: the lane of the chain's most urgent
// member, so the bottom is read first wherever the work is.
func stacked(rows []PullRequest, facts map[string]Facts, known map[string]Known) map[string]Lane {
	var (
		nodes []stack.Node
		keys  []string
	)

	for i := range rows {
		p := &rows[i]
		key := keyOf(p)
		f, ok := facts[key]

		if !ok || known[key].Lane == LaneMerged {
			continue
		}

		nodes = append(nodes, stack.Node{Repository: p.Repository, Number: p.Number, Base: f.Base, Head: f.Head})
		keys = append(keys, key)
	}

	into := map[string]Lane{}

	for _, chain := range stack.Chains(nodes) {
		lane := known[keys[chain[0]]].Lane
		for _, at := range chain {
			lane = min(lane, known[keys[at]].Lane)
		}

		for pos, at := range chain {
			k := known[keys[at]]
			k.Stack = Stack{ID: keys[chain[0]], Position: pos + 1, Size: len(chain)}
			known[keys[at]] = k
			into[keys[at]] = lane
		}
	}

	return into
}

// Arrange puts items in order with each stack drawn as one unit: the chain
// stands where its bottom ranks, bottom first, and the rows above it are not
// ranked at all.
func Arrange(items []PullRequest, known func(*PullRequest) Known, order Order) {
	bottoms := map[string]bool{}
	for i := range items {
		if k := known(&items[i]); k.Stack.Size > 0 && !k.Stack.Above() {
			bottoms[k.Stack.ID] = true
		}
	}

	var (
		leads []PullRequest
		above = map[string][]PullRequest{}
	)

	for i := range items {
		if k := known(&items[i]); k.Stack.Above() && bottoms[k.Stack.ID] {
			above[k.Stack.ID] = append(above[k.Stack.ID], items[i])
		} else {
			leads = append(leads, items[i])
		}
	}

	order(leads, known)

	out := items[:0]

	for i := range leads {
		out = append(out, leads[i])

		id := known(&leads[i]).Stack.ID
		chain := above[id]

		slices.SortStableFunc(chain, func(a, b PullRequest) int {
			return cmp.Compare(known(&a).Stack.Position, known(&b).Stack.Position)
		})

		out = append(out, chain...)
	}
}
