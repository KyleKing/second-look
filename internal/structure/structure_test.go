package structure_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/structure"
)

// lines splits a fragment written as a raw string, which is how a hunk's two
// sides read in a test.
func lines(s string) []string {
	return strings.Split(strings.TrimPrefix(s, "\n"), "\n")
}

// TestReadWithoutAParser pins what the text-only half answers, which is the
// half that runs everywhere: CI has no ast-grep and neither does a fresh
// laptop.
func TestReadWithoutAParser(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		path          string
		before, after string
		want          structure.Change
	}{
		{
			name: "a re-indent changes nothing",
			path: "a.py",
			before: `
def total(rows):
  n = 0
  return n`,
			after: `
def total(rows):
    n = 0
    return n`,
			want: structure.ChangeLayout,
		},
		{
			name: "a re-wrap moves the line boundaries and nothing else",
			path: "a.py",
			before: `
total = compute(rows, base, limit)`,
			after: `
total = compute(
    rows,
    base,
    limit
)`,
			want: structure.ChangeLayout,
		},
		{
			name: "a re-wrap that gains a trailing comma is a change",
			path: "a.py",
			before: `
total = compute(rows, base, limit)`,
			after: `
total = compute(
    rows,
    base,
    limit,
)`,
			want: structure.ChangeCode,
		},
		{
			name: "a character gained is a change",
			path: "a.py",
			before: `
n = 0`,
			after: `
n = 1`,
			want: structure.ChangeCode,
		},
		{
			name: "a file with no grammar is read as changed",
			path: "notes.txt",
			before: `
one`,
			after: `
two`,
			want: structure.ChangeCode,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := structure.Read(t.Context(), structure.Hunk{
				Path: c.path, Before: lines(c.before), After: lines(c.after),
			})
			if err != nil {
				t.Fatalf("reading the hunk: %v", err)
			}

			if got.Change != c.want {
				t.Errorf("change = %v, want %v", got.Change, c.want)
			}
		})
	}
}

// parsed is one case for the pass the grammar answers. The table lives outside
// the test because the fragments are the length here.
type parsed struct {
	name          string
	path          string
	before, after string
	change        structure.Change
	kind          structure.Kind
	gained        []structure.Class
	// symbols is each declaration the hunk touched as "name:kind", left nil
	// where the case is not about naming one.
	symbols []string
}

// check compares one reading against what the case expects.
func (c parsed) check(t *testing.T, got structure.Reading) {
	t.Helper()

	if got.Change != c.change {
		t.Errorf("change = %v, want %v", got.Change, c.change)
	}

	if got.Kind != c.kind {
		t.Errorf("kind = %v, want %v", got.Kind, c.kind)
	}

	if !slices.Equal(got.Gained, c.gained) {
		t.Errorf("gained = %v, want %v", got.Gained, c.gained)
	}

	if c.symbols == nil {
		return
	}

	named := make([]string, 0, len(got.Symbols))
	for _, sym := range got.Symbols {
		named = append(named, sym.Name+":"+sym.Kind.String())
	}

	if !slices.Equal(named, c.symbols) {
		t.Errorf("symbols = %v, want %v", named, c.symbols)
	}
}

var parsedCases = []parsed{
	{
		name: "only the comment changed",
		path: "a.py",
		before: `
def total(rows):
    # old wording
    return sum(rows)`,
		after: `
def total(rows):
    # new wording
    return sum(rows)`,
		change: structure.ChangeComment,
		kind:   structure.KindBody,
	},
	{
		name: "the parameters changed",
		path: "a.py",
		before: `
def total(rows):
    return sum(rows)`,
		after: `
def total(rows, base):
    return sum(rows) + base`,
		change:  structure.ChangeCode,
		kind:    structure.KindSignature,
		symbols: []string{"def total:signature"},
	},
	{
		name: "a function arrived",
		path: "a.go",
		before: `
func Total(rows []int) int {
	return 0
}`,
		after: `
func Total(rows []int) int {
	return 0
}

func Reset() {
}`,
		change:  structure.ChangeCode,
		kind:    structure.KindNew,
		symbols: []string{"func Reset:new"},
	},
	{
		name: "a function went",
		path: "a.go",
		before: `
func Total(rows []int) int {
	return 0
}

func Reset() {
}`,
		after: `
func Total(rows []int) int {
	return 0
}`,
		change:  structure.ChangeCode,
		kind:    structure.KindDeleted,
		symbols: []string{"func Reset:deleted"},
	},
	{
		name: "the body reached for a shell",
		path: "a.py",
		before: `
def run(cmd):
    return log(cmd)`,
		after: `
def run(cmd):
    return subprocess.run(cmd)`,
		change: structure.ChangeCode,
		kind:   structure.KindBody,
		gained: []structure.Class{structure.ClassExec},
	},
}

// TestReadWithAParser is what the grammar adds: a comment-only change, the four
// symbol kinds, and a capability the after side gained.
func TestReadWithAParser(t *testing.T) {
	t.Parallel()

	needsParser(t)

	for _, c := range parsedCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := structure.Read(t.Context(), structure.Hunk{
				Path: c.path, Before: lines(c.before), After: lines(c.after),
			})
			if err != nil {
				t.Fatalf("reading the hunk: %v", err)
			}

			c.check(t, got)
		})
	}
}

func needsParser(t *testing.T) {
	t.Helper()

	if !structure.Available() {
		t.Skip("ast-grep is not installed")
	}
}
