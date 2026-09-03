package structure

import (
	"context"
	"strings"
)

// Hunk is one hunk's two sides as the patch carries them. The context lines
// belong to both, which is what lets each side parse as code rather than as a
// handful of statements torn out of one.
type Hunk struct {
	Path          string
	Before, After []string
}

// Change is how much of a hunk a parser can see.
type Change int

const (
	// ChangeCode is something the parser sees, and is also the answer when
	// nothing could parse: a hunk nobody can vouch for is a hunk to read.
	ChangeCode Change = iota
	// ChangeLayout is indentation, line boundaries, and blank lines, with every
	// other character of both sides the same.
	ChangeLayout
	// ChangeComment is comments and layout, with the code the same.
	ChangeComment
)

// Cosmetic reports whether a reader can skip this hunk without missing a change
// to the program.
func (c Change) Cosmetic() bool { return c != ChangeCode }

// Reading is one hunk's structural pass.
type Reading struct {
	Change Change
	// Kind is the strongest thing the hunk did to any symbol it touches, which
	// is the signal the review-cost rating multiplies the rest by.
	Kind Kind
	// Symbols is every declaration the hunk touched and what it did to each, in
	// name order. A hunk that only changed a body touches none: the symbol it
	// sits inside is above the fragment and not knowable from it.
	Symbols []Symbol
	// Gained is the capability classes the after side calls into and the before
	// side did not. Its honest meaning is "a new capability visible to syntax".
	Gained []Class
	// Called is every name the after side calls, bare of any receiver, in name
	// order. It is what links a hunk to the hunk declaring what it calls, and
	// like every other answer here it is read off syntax rather than resolved:
	// two functions of one name are one name.
	Called []string
	// Declared is every symbol the after side declares, changed or not, in name
	// order. Symbols is what the hunk did; this is what it is inside, which is
	// what a caller has to be gathered with.
	//
	// A hunk is a fragment, so this is what the fragment shows: a change deep in
	// a body whose declaration is above the hunk declares nothing here.
	Declared []string
	// Parsed says a grammar answered. False means Change came from comparing
	// text and Kind and Gained are empty, which is what an absent ast-grep, an
	// unknown extension, or a fragment nothing could recover from all look like.
	Parsed bool
}

// Read is one hunk's whole structural pass: two subprocesses, three answers.
//
// A layout-only change is settled without a parser, because comparing every
// non-whitespace byte of the two sides answers it exactly and costs nothing.
// Everything past that needs the grammar.
func Read(ctx context.Context, h Hunk) (Reading, error) {
	before, after := strings.Join(h.Before, "\n"), strings.Join(h.After, "\n")

	if bare(before) == bare(after) {
		return Reading{Change: ChangeLayout}, nil
	}

	lang, ok := langFor(h.Path)
	if !ok || !Available() {
		return Reading{Change: ChangeCode}, nil
	}

	was, err := scan(ctx, lang, before)
	if err != nil {
		return Reading{Change: ChangeCode}, err
	}

	now, err := scan(ctx, lang, after)
	if err != nil {
		return Reading{Change: ChangeCode}, err
	}

	syms := symbolsOf(was, now)
	r := Reading{
		Change:   ChangeCode,
		Kind:     kindOf(syms),
		Symbols:  syms,
		Gained:   gained(was, now),
		Called:   called(now),
		Declared: declaredIn(now),
		Parsed:   true,
	}

	if bare(without(before, was)) == bare(without(after, now)) {
		r.Change = ChangeComment
	}

	return r, nil
}

// without cuts the comments out of a fragment, so what is left is the code the
// two sides are compared on. The ranges are byte offsets into this same source
// and ast-grep reports them innermost last, so they are applied back to front.
func without(src string, ms []match) string {
	out := []byte(src)

	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		if m.Rule != ruleComment || m.Start < 0 || m.End > len(out) || m.Start > m.End {
			continue
		}

		out = append(out[:m.Start:m.Start], out[m.End:]...)
	}

	return string(out)
}

// bare is every character of a fragment that is not whitespace. Comparing two
// sides on it makes a re-indent, a re-wrap, and a blank line all read as layout,
// where comparing line by line calls a re-wrap a change.
func bare(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case ' ', '\t', '\r', '\n', '\f', '\v':
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
