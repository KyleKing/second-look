// Package stack finds pull requests stacked on one another: each one's base
// branch is the head branch of the one beneath it. Reading the top of a stack
// before the bottom means reading a diff against changes you have not seen, so
// every list that shows a chain shows it bottom first.
package stack

import "sort"

// Node is a pull request as the rule reads it. A branch name means something
// only within its repository, since "main" names a different branch in each.
type Node struct {
	Repository string
	Number     int
	Base, Head string
}

// shortest is the length of the smallest chain. A chain of one is a pull request
// standing on its own, whatever its base names.
const shortest = 2

// Chains returns each stack among nodes as indexes into nodes, bottom first and
// in the order the bottoms first appear. Where a stack forks, the branches read
// in number order, one after the other. A node carrying no base cannot sit in a
// stack, and one carrying no head cannot have anything above it.
func Chains(nodes []Node) [][]int {
	heads := make(map[string]int, len(nodes))

	for i := range nodes {
		if nodes[i].Head != "" {
			heads[key(&nodes[i], nodes[i].Head)] = i
		}
	}

	next := children(nodes, heads)
	taken := make([]bool, len(nodes))

	var out [][]int

	for i := range nodes {
		if taken[i] || !bottom(nodes, heads, i) {
			continue
		}

		chain := walk(next, make(map[int]bool), i)
		if len(chain) < shortest {
			continue
		}

		for _, at := range chain {
			taken[at] = true
		}

		out = append(out, chain)
	}

	return out
}

// children maps each node to the ones on top of it, in number order so a stack
// that forks reads the same way twice.
func children(nodes []Node, heads map[string]int) map[int][]int {
	next := make(map[int][]int)

	for i := range nodes {
		if nodes[i].Base == "" {
			continue
		}

		if under, ok := heads[key(&nodes[i], nodes[i].Base)]; ok && under != i {
			next[under] = append(next[under], i)
		}
	}

	for under := range next {
		sort.SliceStable(next[under], func(a, b int) bool {
			return nodes[next[under][a]].Number < nodes[next[under][b]].Number
		})
	}

	return next
}

// bottom reports a node nothing sits under, which is where a chain is read from.
func bottom(nodes []Node, heads map[string]int, i int) bool {
	if nodes[i].Base == "" {
		return false
	}

	under, ok := heads[key(&nodes[i], nodes[i].Base)]

	return !ok || under == i
}

// walk collects the chain above i, depth first. The seen set guards against a
// pair of nodes naming each other, which would otherwise never end.
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

func key(n *Node, branch string) string { return n.Repository + "\x00" + branch }
