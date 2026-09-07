package order_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/order"
)

// shown renders a plan the way a reader sees it, so a test asserts on the order
// rather than on the shape it is carried in.
func shown(groups []order.Group) string {
	var b strings.Builder

	for _, g := range groups {
		b.WriteString(g.Name + ":")

		for _, h := range g.Hunks {
			fmt.Fprintf(&b, " %s#%d", h.Path, h.Hunk)
		}

		b.WriteString("\n")
	}

	return b.String()
}

func hunk(path string, n int, declares, calls []string) order.Hunk {
	dir := "."
	if at := strings.LastIndex(path, "/"); at > 0 {
		dir = path[:at]
	}

	return order.Hunk{
		Ref: order.Ref{Path: path, Hunk: n}, Dir: dir, Declares: declares, Calls: calls,
	}
}

// scenarios is the shapes a diff arrives in, which the invariants below are
// each asserted against in turn: what holds on one diff and not on another is
// not an invariant.
func scenarios() []struct {
	name  string
	hunks []order.Hunk
} {
	generated := hunk("uv.lock", 1, nil, nil)
	generated.Made = true

	dear := hunk("a/dear.go", 1, []string{"Signature"}, nil)
	dear.Cost = 60

	return []struct {
		name  string
		hunks []order.Hunk
	}{
		{"nothing links", []order.Hunk{
			hunk("a/one.go", 1, nil, nil),
			hunk("b/two.go", 1, nil, nil),
			hunk("a/one.go", 2, nil, nil),
		}},
		{"one file in many hunks", []order.Hunk{
			hunk("a/one.go", 1, nil, nil),
			hunk("a/one.go", 2, []string{"Middle"}, nil),
			hunk("a/one.go", 3, nil, nil),
			hunk("b/two.go", 1, nil, []string{"Middle"}),
		}},
		{"a caller in another directory", []order.Hunk{
			hunk("internal/budget/read.go", 1, []string{"ReadBudget"}, nil),
			hunk("internal/other/thing.go", 1, nil, nil),
			hunk("cmd/app/main.go", 1, nil, []string{"ReadBudget"}),
		}},
		{"a name too common to link", []order.Hunk{
			hunk("a/one.go", 1, []string{"New"}, nil),
			hunk("b/two.go", 1, []string{"New"}, nil),
			hunk("c/three.go", 1, nil, []string{"New"}),
		}},
		{"a link nothing answers", []order.Hunk{
			hunk("a/one.go", 1, []string{"Alone"}, nil),
			hunk("b/two.go", 1, nil, nil),
		}},
		{"cost reorders the groups", []order.Hunk{
			hunk("a/cheap.go", 1, []string{"Rename"}, nil),
			hunk("b/calls.go", 1, nil, []string{"Rename"}),
			dear,
			hunk("b/more.go", 1, nil, []string{"Signature"}),
		}},
		{"what a machine wrote", []order.Hunk{
			hunk("a/read.go", 1, []string{"ReadBudget"}, nil),
			generated,
			hunk("a/call.go", 1, nil, []string{"ReadBudget"}),
		}},
	}
}

// The plan is a stable partition: it decides which group a hunk belongs to and
// never which of two hunks comes first. So every group reads in the order the
// diff named its hunks, and the only rows that moved are the ones a heading
// gathered.
//
// This is the whole of what "the order is disturbed as little as possible"
// means. A cost sort inside a group, or a file's hunks emitted by size, would
// each be a real improvement to argue for and would each break this.
func TestAGroupKeepsTheDiffsOwnOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range scenarios() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			at := map[order.Ref]int{}
			for i, h := range tc.hunks {
				at[h.Ref] = i
			}

			for _, g := range order.Plan(tc.hunks) {
				last := -1

				for _, h := range g.Hunks {
					if at[h] <= last {
						t.Errorf("%s reads %v after the hunk at %d, which the diff put later",
							g.Name, h, last)
					}

					last = at[h]
				}
			}
		})
	}
}

// pieces is how many places each file is drawn in, and how many of those a
// symbol gathered it into.
func pieces(groups []order.Group) (map[string]int, map[string]int) {
	parts, gathered := map[string]int{}, map[string]int{}

	for _, g := range groups {
		in := map[string]bool{}

		for _, h := range g.Hunks {
			if !in[h.Path] {
				in[h.Path] = true
				parts[h.Path]++
			}

			if g.Symbol {
				gathered[h.Path]++
			}
		}
	}

	return parts, gathered
}

// Two hunks of one file are never drawn in two places unless a symbol gathered
// one of them. Splitting a file is what costs a reader the most, so it happens
// exactly as often as gathering earns it and no more.
func TestAFileIsSplitOnlyWhereASymbolGathersIt(t *testing.T) {
	t.Parallel()

	for _, tc := range scenarios() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			groups := order.Plan(tc.hunks)
			parts, gathered := pieces(groups)

			for path, n := range parts {
				// A file drawn in two places has at least one hunk in a symbol
				// group, and one more piece for each further symbol that took one.
				if want := gathered[path] + 1; n > want {
					t.Errorf("%s is drawn in %d places, want at most %d:\n%s",
						path, n, want, shown(groups))
				}
			}
		})
	}
}

// Two runs over one review agree. Names are gathered out of a map, so an order
// that leaned on iteration would put a review in a different shape every time
// it was opened and lose the reader their place on every rebuild.
func TestThePlanIsTheSameEveryTime(t *testing.T) {
	t.Parallel()

	for _, tc := range scenarios() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := shown(order.Plan(tc.hunks))

			for range 20 {
				if got := shown(order.Plan(tc.hunks)); got != want {
					t.Fatalf("a second plan is\n%swant\n%s", got, want)
				}
			}
		})
	}
}

// A callee whose signature moved and the caller that has to change with it are
// pages apart in the diff and next to each other here, whatever directories
// they came from.
func TestACallerIsGatheredWithItsCallee(t *testing.T) {
	t.Parallel()

	got := shown(order.Plan([]order.Hunk{
		hunk("internal/budget/read.go", 1, []string{"ReadBudget"}, nil),
		hunk("internal/other/thing.go", 2, []string{"Unrelated"}, nil),
		hunk("cmd/app/main.go", 3, nil, []string{"ReadBudget"}),
	}))

	want := "ReadBudget: internal/budget/read.go#1 cmd/app/main.go#3\n" +
		"internal/other: internal/other/thing.go#2\n"

	if got != want {
		t.Errorf("plan is\n%swant\n%s", got, want)
	}
}

// A name several hunks declare is `New` or `Error`, which is one symbol only to
// a matcher. Gathering on it would put half a diff under a heading that lies.
func TestACommonNameGathersNothing(t *testing.T) {
	t.Parallel()

	got := shown(order.Plan([]order.Hunk{
		hunk("a/one.go", 1, []string{"New"}, nil),
		hunk("b/two.go", 2, []string{"New"}, nil),
		hunk("c/three.go", 3, nil, []string{"New"}),
	}))

	want := "a: a/one.go#1\nb: b/two.go#2\nc: c/three.go#3\n"
	if got != want {
		t.Errorf("plan is\n%swant\n%s", got, want)
	}
}

// Every hunk handed in comes back exactly once, whatever links to what: a hunk
// that vanished from the reading order is a change nobody reviews.
func TestEveryHunkIsPlacedExactlyOnce(t *testing.T) {
	t.Parallel()

	in := []order.Hunk{
		hunk("a/one.go", 1, []string{"Read"}, []string{"Write"}),
		hunk("a/two.go", 2, []string{"Write"}, []string{"Read"}),
		hunk("b/three.go", 3, nil, []string{"Read", "Write"}),
		hunk("b/four.go", 4, nil, nil),
	}

	seen := map[order.Ref]int{}
	for _, g := range order.Plan(in) {
		for _, h := range g.Hunks {
			seen[h]++
		}
	}

	if len(seen) != len(in) {
		t.Errorf("the plan carries %d hunks, want %d: %v", len(seen), len(in), seen)
	}

	for ref, n := range seen {
		if n != 1 {
			t.Errorf("%v appears %d times", ref, n)
		}
	}
}

// The costliest symbol is read first, and equal costs keep the order the diff
// declared them in so two runs over one review agree.
func TestGroupsAreOrderedByWhatTheyCost(t *testing.T) {
	t.Parallel()

	cheap := hunk("a/cheap.go", 1, []string{"Rename"}, nil)
	dear := hunk("a/dear.go", 3, []string{"Signature"}, nil)
	dear.Cost = 60

	got := shown(order.Plan([]order.Hunk{
		cheap,
		hunk("b/calls.go", 2, nil, []string{"Rename"}),
		dear,
		hunk("b/more.go", 4, nil, []string{"Signature"}),
	}))

	if !strings.HasPrefix(got, "Signature:") {
		t.Errorf("the costly symbol is not first:\n%s", got)
	}
}

// A directory group with more to read comes before one with less, the same
// way a symbol group does, and equal costs keep the order the diff declared
// them in so two runs over one review agree.
func TestDirectoryGroupsAreOrderedByWhatTheyCost(t *testing.T) {
	t.Parallel()

	dear := hunk("b/dear.go", 1, nil, nil)
	dear.Cost = 60

	got := shown(order.Plan([]order.Hunk{
		hunk("a/cheap.go", 1, nil, nil),
		dear,
		hunk("b/also.go", 2, nil, nil),
	}))

	if !strings.HasPrefix(got, "b:") {
		t.Errorf("the costlier directory is not first:\n%s", got)
	}
}

// What a machine wrote is one group at the end, and no symbol gathers it: a
// lockfile that happens to name a function is still a lockfile.
func TestGeneratedGoesLastAndIsNeverGathered(t *testing.T) {
	t.Parallel()

	made := hunk("uv.lock", 2, nil, []string{"ReadBudget"})
	made.Made = true

	groups := order.Plan([]order.Hunk{
		hunk("a/read.go", 1, []string{"ReadBudget"}, nil),
		made,
		hunk("a/call.go", 3, nil, []string{"ReadBudget"}),
	})

	last := groups[len(groups)-1]
	if last.Name != order.Generated || !last.Made {
		t.Fatalf("the last group is %q, want the generated one", last.Name)
	}

	if len(last.Hunks) != 1 || last.Hunks[0].Path != "uv.lock" {
		t.Errorf("the generated group holds %v", last.Hunks)
	}

	if got := shown(groups[:1]); got != "ReadBudget: a/read.go#1 a/call.go#3\n" {
		t.Errorf("the symbol group is\n%s", got)
	}
}
