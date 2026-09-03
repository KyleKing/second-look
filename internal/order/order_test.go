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
