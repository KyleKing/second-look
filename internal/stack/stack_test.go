package stack_test

import (
	"fmt"
	"testing"

	"github.com/kyleking/second-look/internal/stack"
)

func pr(repo string, n int, base, head string) stack.Node {
	return stack.Node{Repository: repo, Number: n, Base: base, Head: head}
}

func TestChains(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		nodes []stack.Node
		want  string
	}{
		{
			"a chain listed bottom first",
			[]stack.Node{pr("o/r", 1, "main", "a"), pr("o/r", 2, "a", "b"), pr("o/r", 3, "b", "c")},
			"[[0 1 2]]",
		},
		{
			"a chain listed top first is still read from its bottom, once",
			[]stack.Node{pr("o/r", 3, "b", "c"), pr("o/r", 2, "a", "b"), pr("o/r", 1, "main", "a")},
			"[[2 1 0]]",
		},
		{
			"a fork reads its branches in number order",
			[]stack.Node{pr("o/r", 1, "main", "a"), pr("o/r", 3, "a", "c"), pr("o/r", 2, "a", "b")},
			"[[0 2 1]]",
		},
		{
			"two stacks keep the order their bottoms appear in",
			[]stack.Node{
				pr("o/r", 5, "x", "y"), pr("o/r", 1, "main", "a"),
				pr("o/r", 4, "main", "x"), pr("o/r", 2, "a", "b"),
			},
			"[[1 3] [2 0]]",
		},
		{"a pull request on its own", []stack.Node{pr("o/r", 1, "main", "a")}, "[]"},
		{
			"a branch name means nothing across repositories",
			[]stack.Node{pr("o/one", 1, "main", "a"), pr("o/two", 2, "a", "b")},
			"[]",
		},
		{
			"a pair naming each other has no bottom",
			[]stack.Node{pr("o/r", 1, "b", "a"), pr("o/r", 2, "a", "b")},
			"[]",
		},
		{
			"a node with no base cannot sit in a stack",
			[]stack.Node{pr("o/r", 1, "", "a"), pr("o/r", 2, "a", "b")},
			"[]",
		},
		{
			"a node with no head has nothing above it",
			[]stack.Node{pr("o/r", 1, "main", ""), pr("o/r", 2, "", "b")},
			"[]",
		},
		{
			"a node naming its own head as base stands at the bottom",
			[]stack.Node{pr("o/r", 1, "a", "a"), pr("o/r", 2, "a", "b")},
			"[[0 1]]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := fmt.Sprint(stack.Chains(tc.nodes)); got != tc.want {
				t.Errorf("Chains = %s, want %s", got, tc.want)
			}
		})
	}
}
