package tui_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/structure"
	"github.com/kyleking/second-look/internal/tui"
)

// reading is one hunk's structural answer, built by hand so a move can be
// checked without an ast-grep run over a real diff.
func reading(syms ...structure.Symbol) structure.Reading {
	return structure.Reading{Parsed: true, Symbols: syms}
}

// sym names a symbol the way the structural pass does, by the declaration head
// up to its first delimiter.
func sym(head string, k structure.Kind) structure.Symbol {
	name, _, _ := strings.Cut(head, "(")

	return structure.Symbol{Name: name, Ident: name, Head: head, Kind: k}
}

func TestMovedSymbols(t *testing.T) {
	t.Parallel()

	const head = "func ReadBudget(path string) (int, error) {"

	tests := []struct {
		name     string
		readings []structure.Reading
		refs     []tui.HunkRef
		want     []string
	}{
		{
			// A symbol matched across files, which is the reading that stops a
			// move being drawn as a delete beside an unrelated insert.
			name: "across files",
			readings: []structure.Reading{
				reading(sym(head, structure.KindDeleted)),
				reading(sym(head, structure.KindNew)),
			},
			refs: []tui.HunkRef{{Path: "budget/old.go", Hunk: 1}, {Path: "budget/new.go", Hunk: 2}},
			want: []string{"ReadBudget moved to budget/new.go", "ReadBudget moved from budget/old.go"},
		},
		{
			name: "within one file",
			readings: []structure.Reading{
				reading(sym(head, structure.KindDeleted)),
				reading(sym(head, structure.KindNew)),
			},
			refs: []tui.HunkRef{{Path: "budget/one.go", Hunk: 1}, {Path: "budget/one.go", Hunk: 2}},
			want: []string{"ReadBudget moved out", "ReadBudget moved in"},
		},
		{
			// A declaration rewritten on the way is not the same code arriving
			// somewhere else, which is what the head in the key buys.
			name: "rewritten on the way",
			readings: []structure.Reading{
				reading(sym(head, structure.KindDeleted)),
				reading(sym("func ReadBudget(path string) (Budget, error) {", structure.KindNew)),
			},
			refs: []tui.HunkRef{{Path: "budget/old.go", Hunk: 1}, {Path: "budget/new.go", Hunk: 2}},
			want: []string{"ReadBudget deleted", "ReadBudget new"},
		},
		{
			// Two files deleting the same declaration and one adding it back
			// gives no way to say which one moved, so neither does.
			name: "ambiguous",
			readings: []structure.Reading{
				reading(sym(head, structure.KindDeleted)),
				reading(sym(head, structure.KindDeleted)),
				reading(sym(head, structure.KindNew)),
			},
			refs: []tui.HunkRef{
				{Path: "a.go", Hunk: 1}, {Path: "b.go", Hunk: 2}, {Path: "c.go", Hunk: 3},
			},
			want: []string{"ReadBudget deleted", "ReadBudget deleted", "ReadBudget new"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tui.SymbolWords(tc.readings, tc.refs)
			for i := range tc.want {
				if !strings.Contains(got[i], tc.want[i]) {
					t.Errorf("hunk %d says %q, want it to carry %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestFileWordCallsAMoveAMove is the file summary a reader decides what to open
// from: the file a symbol left says "moved" rather than "deleted", so nothing
// reads as gone when it is somewhere else in the same pull request.
func TestFileWordCallsAMoveAMove(t *testing.T) {
	t.Parallel()

	const head = "func ReadBudget(path string) (int, error) {"

	readings := []structure.Reading{
		reading(sym(head, structure.KindDeleted)),
		reading(sym(head, structure.KindNew)),
	}
	refs := []tui.HunkRef{{Path: "budget/old.go", Hunk: 1}, {Path: "budget/new.go", Hunk: 2}}

	for _, path := range []string{"budget/old.go", "budget/new.go"} {
		if got := tui.FileWord(readings, refs, path); got != "func ReadBudget moved" {
			t.Errorf("%s summarizes as %q, want the move", path, got)
		}
	}
}
