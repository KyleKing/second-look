package structure

import (
	"slices"
	"strings"
)

// Kind is what a hunk did to the symbols it touches.
type Kind int

const (
	// KindBody is a change inside a symbol whose declaration is untouched.
	KindBody Kind = iota
	// KindDeleted is a symbol the after side no longer declares.
	KindDeleted
	// KindNew is a symbol the before side did not declare.
	KindNew
	// KindSignature is a symbol both sides declare and spell differently: its
	// name, parameters, or return changed, so every caller of it is in scope
	// whether or not the diff shows one.
	KindSignature
)

// String names the kind for a screen. The order the constants are declared in
// is the order they escalate, so the strongest thing a hunk did is its max.
func (k Kind) String() string {
	switch k {
	case KindSignature:
		return "signature"
	case KindNew:
		return "new"
	case KindDeleted:
		return "deleted"
	case KindBody:
		return "body"
	}

	return "body"
}

// Symbol is one declaration a hunk touched and what it did to it. The name is
// the declaration head up to its first delimiter, so a changed parameter list
// still reads as the same symbol.
type Symbol struct {
	Name string
	Kind Kind
	// Head is the declaration's first line with its whitespace squeezed out,
	// which is what tells a symbol that moved from one that was rewritten: the
	// same head in two places is the same declaration.
	Head string
}

// symbolsOf compares the declarations the two sides carry, one entry per symbol
// either side names.
//
// A declaration is identified by its first line, which is where every language
// here puts the name and the parameters. So the same symbol declared differently
// is a signature change, while a symbol only one side declares is new or
// deleted.
func symbolsOf(was, now []match) []Symbol {
	before, after := declared(was), declared(now)

	var out []Symbol

	for name, at := range after {
		prior, ok := before[name]

		switch {
		case !ok:
			out = append(out, symbol(at, KindNew))
		case !sameHeads(prior.heads, at.heads):
			out = append(out, symbol(at, KindSignature))
		}
	}

	for name, at := range before {
		if _, ok := after[name]; !ok {
			out = append(out, symbol(at, KindDeleted))
		}
	}

	slices.SortFunc(out, func(a, b Symbol) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// kindOf is the strongest thing a hunk did to any symbol, which is what the
// review-cost rating multiplies the rest by. The constants escalate in the
// order they are declared, so it is the maximum.
func kindOf(syms []Symbol) Kind {
	out := KindBody
	for _, s := range syms {
		out = max(out, s.Kind)
	}

	return out
}

// symbol names a declaration the way a person reads it, keeping the squeezed
// head so a symbol that moved can be told from one that was rewritten.
func symbol(at decl, k Kind) Symbol {
	return Symbol{Name: nameOf(at.shown), Kind: k, Head: at.heads[0]}
}

func sameHeads(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)

	return slices.Equal(x, y)
}

// decl is what a side says about one symbol: every spelling of its declaration,
// squeezed so two of them compare by their code rather than by their layout,
// and the first line as written, which is what a screen shows.
type decl struct {
	heads []string
	shown string
}

// declared is the declarations a side carries, grouped by symbol name.
func declared(ms []match) map[string]decl {
	byName := map[string]decl{}

	for _, m := range ms {
		if m.Rule != ruleDecl {
			continue
		}

		line, _, _ := strings.Cut(m.Text, "\n")

		h := bare(line)
		if h == "" {
			continue
		}

		at := byName[nameOf(h)]
		at.heads = append(at.heads, h)

		if at.shown == "" {
			at.shown = strings.TrimSpace(line)
		}

		byName[nameOf(h)] = at
	}

	return byName
}

// nameOf is a declaration head up to the first delimiter, so a changed
// parameter list still reads as the same symbol.
func nameOf(head string) string {
	if i := strings.IndexAny(head, "({<:=[|"); i >= 0 {
		head = head[:i]
	}

	return strings.TrimSpace(head)
}
