package prepared

import (
	"sort"
	"strconv"
)

// Stack is a chain of staged reviews, bottom first: each one's base branch is
// the head of the one before it. Reading the top of a stack before the bottom
// means reading a diff against changes you have not seen, so a list that shows
// the chain shows what order to read it in.
//
// A stack is only visible where both of its pull requests have a review staged
// here. That is the limit of reading the artifact alone, and it is the case
// that matters: a stack nobody is reviewing needs no order.
type Stack struct {
	// Onto is the branch the bottom of the stack merges into, which is what
	// says where the whole chain lands.
	Onto string
	Rows []Review
}

// Split separates the staged reviews into stacks and everything standing on its
// own, keeping the order they came in. A review staged before the branches were
// recorded carries none, so it can only ever stand alone.
func Split(rows []Review) ([]Stack, []Review) {
	heads := make(map[string]int, len(rows))

	for i := range rows {
		if rows[i].HeadRef != "" {
			heads[key(&rows[i], rows[i].HeadRef)] = i
		}
	}

	next := children(rows, heads)

	// A chain of one is a review standing on its own, whatever its base names.
	const shortest = 2

	var (
		stacks []Stack
		alone  []Review
		taken  = make([]bool, len(rows))
	)

	for i := range rows {
		if taken[i] || !bottom(rows, heads, i) {
			continue
		}

		chain := walk(next, make(map[int]bool), i)
		if len(chain) < shortest {
			continue
		}

		for _, at := range chain {
			taken[at] = true
		}

		stacks = append(stacks, Stack{Onto: rows[i].BaseRef, Rows: pick(rows, chain)})
	}

	for i := range rows {
		if !taken[i] {
			alone = append(alone, rows[i])
		}
	}

	return stacks, alone
}

// children maps each review to the ones staged on top of it, in number order so
// a stack that forks reads the same way twice.
func children(rows []Review, heads map[string]int) map[int][]int {
	next := make(map[int][]int)

	for i := range rows {
		if rows[i].BaseRef == "" {
			continue
		}

		if under, ok := heads[key(&rows[i], rows[i].BaseRef)]; ok && under != i {
			next[under] = append(next[under], i)
		}
	}

	for under := range next {
		sort.SliceStable(next[under], func(a, b int) bool {
			return rows[next[under][a]].Number < rows[next[under][b]].Number
		})
	}

	return next
}

// bottom reports a review nothing staged here sits under, which is where a
// chain is read from.
func bottom(rows []Review, heads map[string]int, i int) bool {
	if rows[i].BaseRef == "" {
		return false
	}

	under, ok := heads[key(&rows[i], rows[i].BaseRef)]

	return !ok || under == i
}

// Walk collects the chain above i, depth first. The seen set guards against a
// pair of reviews naming each other, which would otherwise never end.
func walk(next map[int][]int, seen map[int]bool, i int) []int {
	if seen[i] {
		return nil
	}

	seen[i] = true
	out := []int{i}

	for _, above := range next[i] {
		out = append(out, walk(next, seen, above)...)
	}

	return out
}

func pick(rows []Review, chain []int) []Review {
	out := make([]Review, 0, len(chain))
	for _, i := range chain {
		out = append(out, rows[i])
	}

	return out
}

// key scopes a branch name to its repository, since "main" names a different
// branch in every repository.
func key(r *Review, ref string) string {
	return r.Repository + "\x00" + ref
}

// Order is the staged reviews in the order they are read: each stack's chain
// contiguous and bottom first, standing where its earliest row already stood,
// and everything else left where it was. The screen draws the chain itself, so
// this is for the piped list, which is read by whatever reviews them in turn.
func Order(rows []Review) []Review {
	stacks, _ := Split(rows)
	if len(stacks) == 0 {
		return rows
	}

	of := make(map[string]int, len(rows))

	for i := range stacks {
		for j := range stacks[i].Rows {
			of[id(&stacks[i].Rows[j])] = i
		}
	}

	out := make([]Review, 0, len(rows))
	drawn := make([]bool, len(stacks))

	for i := range rows {
		at, ok := of[id(&rows[i])]
		if !ok {
			out = append(out, rows[i])

			continue
		}

		if !drawn[at] {
			drawn[at] = true

			out = append(out, stacks[at].Rows...)
		}
	}

	return out
}

// id names one staged review, since a number alone repeats across repositories.
func id(r *Review) string { return r.Repository + "\x00" + strconv.Itoa(r.Number) }
