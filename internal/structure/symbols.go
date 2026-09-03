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
	// Ident is the bare identifier, which is what a call to this symbol spells:
	// Name carries the keyword that introduced the declaration, and a call
	// carries none. It is what links a caller to its callee.
	Ident string
	Kind  Kind
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
	name := nameOf(at.shown)

	return Symbol{Name: name, Ident: identOf(name), Kind: k, Head: at.heads[0]}
}

// declWords are the words that introduce a declaration rather than name one,
// across the grammars here. A head is several of them followed by the name.
var declWords = map[string]bool{
	"abstract": true, "async": true, "class": true, "const": true, "data": true,
	"def": true, "default": true, "enum": true, "export": true, "extern": true,
	"final": true, "fn": true, "func": true, "function": true, "impl": true,
	"inline": true, "interface": true, "internal": true, "let": true,
	"module": true, "override": true, "package": true, "private": true,
	"protected": true, "public": true, "record": true, "static": true,
	"struct": true, "sub": true, "trait": true, "type": true, "var": true,
	"virtual": true, "void": true,
}

// identOf is the bare identifier inside a declaration head: the first word that
// introduces nothing.
//
// A return type is a word too, and in `public static void main` it is one of
// these; where a language puts a type nothing knows about in front of the name,
// this answers the type instead. That costs a link rather than making a wrong
// one, since a call to that name is what would have to exist for it to matter.
func identOf(name string) string {
	fields := strings.Fields(name)

	for _, f := range fields {
		if !declWords[f] {
			return f
		}
	}

	if len(fields) == 0 {
		return name
	}

	return fields[len(fields)-1]
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

// declaredIn is every symbol a side declares, changed or not, by the identifier
// a call to it would spell. It is what gathers a hunk with the hunks that call
// what it is inside, where symbolsOf answers only what the hunk did.
func declaredIn(ms []match) []string {
	var out []string

	// The map is keyed by the squeezed head, which compares two declarations
	// and reads as `funcWiden`. The identifier comes off the line as written.
	for _, at := range declared(ms) {
		if id := identOf(nameOf(at.shown)); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}

	slices.Sort(out)

	return out
}
