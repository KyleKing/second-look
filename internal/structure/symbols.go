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

// kindOf compares the declarations the two sides carry.
//
// A declaration is identified by its first line, which is where every language
// here puts the name and the parameters, and named by that line up to the first
// delimiter. So the same symbol declared differently is a signature change,
// while a symbol only one side declares is new or deleted. Each test is a full
// pass, so the answer does not depend on which name is looked at first.
func kindOf(was, now []match) Kind {
	before, after := declared(was), declared(now)

	for name, heads := range after {
		prior, ok := before[name]
		if ok && !sameHeads(prior, heads) {
			return KindSignature
		}
	}

	for name := range after {
		if _, ok := before[name]; !ok {
			return KindNew
		}
	}

	for name := range before {
		if _, ok := after[name]; !ok {
			return KindDeleted
		}
	}

	return KindBody
}

func sameHeads(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)

	return slices.Equal(x, y)
}

// declared is the declaration heads a side carries, grouped by symbol name.
func declared(ms []match) map[string][]string {
	byName := map[string][]string{}

	for _, m := range ms {
		if m.Rule != ruleDecl {
			continue
		}

		if h := head(m.Text); h != "" {
			byName[nameOf(h)] = append(byName[nameOf(h)], h)
		}
	}

	return byName
}

// head is a declaration's first line with its whitespace squeezed out.
func head(text string) string {
	line, _, _ := strings.Cut(text, "\n")

	return bare(line)
}

// nameOf is a declaration head up to the first delimiter, so a changed
// parameter list still reads as the same symbol.
func nameOf(head string) string {
	if i := strings.IndexAny(head, "({<:=[|"); i >= 0 {
		return head[:i]
	}

	return head
}
