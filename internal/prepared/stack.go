package prepared

import (
	"strconv"

	"github.com/kyleking/second-look/internal/stack"
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
	nodes := make([]stack.Node, 0, len(rows))
	for i := range rows {
		nodes = append(nodes, stack.Node{
			Repository: rows[i].Repository, Number: rows[i].Number,
			Base: rows[i].BaseRef, Head: rows[i].HeadRef,
		})
	}

	chains := stack.Chains(nodes)
	stacks := make([]Stack, 0, len(chains))
	taken := make([]bool, len(rows))

	for _, chain := range chains {
		rowsOf := make([]Review, 0, len(chain))

		for _, at := range chain {
			taken[at] = true
			rowsOf = append(rowsOf, rows[at])
		}

		stacks = append(stacks, Stack{Onto: rows[chain[0]].BaseRef, Rows: rowsOf})
	}

	var alone []Review

	for i := range rows {
		if !taken[i] {
			alone = append(alone, rows[i])
		}
	}

	return stacks, alone
}

// Order is the staged reviews in the order they are read: each stack's chain
// contiguous and bottom first, standing where its earliest row already stood,
// and everything else left where it was.
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
