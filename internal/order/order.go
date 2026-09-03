// Package order decides what order a review's hunks are read in.
//
// The diff's own order is the forge's judgment about what to show first, and it
// is not the order a change makes sense in: one edit made in four places is
// shown as four hunks pages apart, and a caller sits nowhere near the callee
// whose signature moved under it.
//
// So hunks that answer for the same symbol are gathered, wherever in the diff
// they came from, and everything the parser had nothing to say about keeps the
// directory grouping it always had. What a machine wrote goes last either way.
package order

import (
	"cmp"
	"slices"
)

// Generated is what the group holding machine-written files is called. It is
// one group however many directories its files came from: what a reader wants
// of them is one glance, not a tour of the tree they sit in.
const Generated = "generated"

// Ref names one hunk of the diff.
type Ref struct {
	Path string
	Hunk int
}

// Hunk is what deciding an order needs to know about one hunk: where it is,
// what it declares, and what it calls.
type Hunk struct {
	Ref
	// Dir is the directory the file sits in, which is the grouping a hunk falls
	// back to when nothing links it to another.
	Dir string
	// Declares is the symbols the hunk added, removed, or respelled. Made is
	// whether the file was written by a machine.
	Declares []string
	Calls    []string
	Made     bool
	// Cost is what reading the hunk is rated, which orders the groups against
	// each other. Zero is unrated and sorts as if it were cheap.
	Cost int
}

// Group is a run of hunks read together, under the heading that says why.
type Group struct {
	// Name is the symbol the group is about, or the directory its hunks share.
	Name string
	// Symbol says which of the two Name is, so a heading can spell it right.
	Symbol bool
	// Made marks the group holding what a machine wrote.
	Made  bool
	Hunks []Ref
}

// tooCommon is how many hunks may declare one name before the name stops
// linking anything.
//
// A name is matched rather than resolved, so `New`, `Error`, and `Get` are one
// symbol across a whole repository. Left alone they gather half a diff into a
// group whose heading is a lie; the guard is that a symbol worth gathering
// around is one a diff declares in one place.
const tooCommon = 1

// worthGathering is the fewest hunks a group is worth drawing. One is the hunk
// on its own, which the directory grouping already says better.
const worthGathering = 2

// Plan is the order to read a diff in, given what the parser saw of each hunk.
//
// A group is a symbol some hunk declares, together with every hunk that calls
// it or declares it elsewhere. Everything left keeps its directory, in the
// order the diff named them, and generated files go last.
//
// Nothing is dropped and nothing appears twice: every hunk handed in comes back
// in exactly one group.
func Plan(hunks []Hunk) []Group {
	declared := declarers(hunks)
	joined := link(hunks, declared)

	var (
		out   []Group
		taken = make([]bool, len(hunks))
	)

	for _, name := range namesInOrder(hunks, declared) {
		members := joined[name]
		if len(members) < worthGathering {
			continue
		}

		var free []int

		for _, at := range members {
			if !taken[at] && !hunks[at].Made {
				free = append(free, at)
			}
		}

		// A group of one is the hunk on its own, which the directory grouping
		// says better.
		if len(free) < worthGathering {
			continue
		}

		refs := make([]Ref, 0, len(free))

		for _, at := range free {
			taken[at] = true

			refs = append(refs, hunks[at].Ref)
		}

		out = append(out, Group{Name: name, Symbol: true, Hunks: refs})
	}

	return append(out, byDirectory(hunks, taken)...)
}

// declarers is which hunks declare each name, dropping the names too many
// hunks declare to mean anything.
func declarers(hunks []Hunk) map[string][]int {
	out := map[string][]int{}

	for i := range hunks {
		for _, name := range hunks[i].Declares {
			if !slices.Contains(out[name], i) {
				out[name] = append(out[name], i)
			}
		}
	}

	for name, at := range out {
		if len(at) > tooCommon {
			delete(out, name)
		}
	}

	return out
}

// link gathers, for each declared name, the hunks that declare it and the hunks
// that call it.
func link(hunks []Hunk, declared map[string][]int) map[string][]int {
	out := make(map[string][]int, len(declared))
	for name, at := range declared {
		out[name] = slices.Clone(at)
	}

	for i := range hunks {
		for _, name := range hunks[i].Calls {
			if _, ok := declared[name]; !ok {
				continue
			}

			if !slices.Contains(out[name], i) {
				out[name] = append(out[name], i)
			}
		}
	}

	for name := range out {
		slices.Sort(out[name])
	}

	return out
}

// namesInOrder is every linkable name, strongest first: a symbol whose hunks
// cost most to read is the one to read first, and equal costs keep the order
// the diff declared them in, so an order is stable between runs.
func namesInOrder(hunks []Hunk, declared map[string][]int) []string {
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}

	cost := map[string]int{}
	first := map[string]int{}

	for name, at := range declared {
		cost[name] = hunks[at[0]].Cost
		first[name] = at[0]
	}

	slices.SortFunc(names, func(a, b string) int {
		if by := cmp.Compare(cost[b], cost[a]); by != 0 {
			return by
		}

		return cmp.Compare(first[a], first[b])
	})

	return names
}

// byDirectory is everything no symbol gathered, under the directory it sits in,
// in the order the diff named them. What a machine wrote is one group at the
// end however many directories it came from.
func byDirectory(hunks []Hunk, taken []bool) []Group {
	var (
		out  []Group
		made Group
		at   = map[string]int{}
	)

	made.Name, made.Made = Generated, true

	for i := range hunks {
		if taken[i] {
			continue
		}

		if hunks[i].Made {
			made.Hunks = append(made.Hunks, hunks[i].Ref)

			continue
		}

		dir := hunks[i].Dir

		j, ok := at[dir]
		if !ok {
			j = len(out)
			at[dir] = j

			out = append(out, Group{Name: dir})
		}

		out[j].Hunks = append(out[j].Hunks, hunks[i].Ref)
	}

	if len(made.Hunks) == 0 {
		return out
	}

	return append(out, made)
}
