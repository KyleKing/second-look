package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kyleking/second-look/internal/generated"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/structure"
)

// shape is what the structural pass found, kept by hunk so a heading can say
// what its hunk did and a file can summarize its own.
//
// It is the same pass the review-cost rating already runs, so the structural
// renderer costs no subprocess of its own: it draws what was read behind the
// first frame.
type shape struct {
	// touched is the symbols each hunk declared, changed, or removed.
	touched map[hunkAt][]structure.Symbol
	// inside is the declarations each hunk sits in, changed or not, which is
	// what a heading says where nothing about them changed.
	inside map[hunkAt][]string
	// moved is the symbols that left one place in a file and arrived in
	// another, by the declaration they both spell.
	moved map[string]move
	// plan is the order the review reads in, worked out from the same pass.
	plan []order.Group
}

// move is one symbol that went from one hunk to another rather than being
// deleted beside an unrelated insert. Drawing it as a delete and an insert asks
// a reader to prove the two are the same code, which is the reading a structural
// pass exists to save.
type move struct {
	name string
	from hunkAt
	to   hunkAt
}

// readShape collects what each hunk did from the pass the rating already ran,
// and works the reading order out of it.
func readShape(readings []structure.Reading, refs []hunkAt, made generated.Set) shape {
	out := shape{
		touched: map[hunkAt][]structure.Symbol{},
		inside:  map[hunkAt][]string{},
		moved:   map[string]move{},
	}

	gone, came := map[string]move{}, map[string]move{}

	for i, r := range readings {
		if i >= len(refs) || !r.Parsed {
			continue
		}

		out.inside[refs[i]] = r.Declared

		if len(r.Symbols) == 0 {
			continue
		}

		out.touched[refs[i]] = r.Symbols

		for _, s := range r.Symbols {
			at := move{name: s.Name, from: refs[i], to: refs[i]}

			switch s.Kind {
			case structure.KindDeleted:
				gone[declKey(refs[i].path, s)] = at
			case structure.KindNew:
				came[declKey(refs[i].path, s)] = at
			case structure.KindBody, structure.KindSignature:
			}
		}
	}

	for k, left := range gone {
		if arrived, ok := came[k]; ok && arrived.to != left.from {
			out.moved[k] = move{name: left.name, from: left.from, to: arrived.to}
		}
	}

	out.plan = order.Plan(planHunks(readings, refs, made))

	return out
}

// planHunks is every hunk as the ordering reads it: where it is, what it
// declares, what it calls, and what reading it costs.
func planHunks(readings []structure.Reading, refs []hunkAt, made generated.Set) []order.Hunk {
	out := make([]order.Hunk, 0, len(refs))

	for i, at := range refs {
		h := order.Hunk{
			Ref:  order.Ref{Path: at.path, Hunk: at.hunk},
			Dir:  dirOf(at.path),
			Made: made.Match(at.path),
		}

		if i < len(readings) && readings[i].Parsed {
			h.Calls = readings[i].Called
			h.Cost = rate.HunkCost(readings[i])

			h.Declares = readings[i].Declared
		}

		out = append(out, h)
	}

	return out
}

// declKey identifies a declaration across the file it moved within. It carries
// the head as well as the name, because a symbol whose declaration was rewritten
// on the way is not the same code arriving somewhere else.
func declKey(path string, s structure.Symbol) string {
	return path + "\x00" + s.Name + "\x00" + s.Head
}

// symbolWord is what a hunk did, for its heading: the symbols it touched and
// how, or nothing where it changed a body whose declaration is above the
// fragment.
//
// That absence is the caveat the mode ships with. A hunk is a fragment of a
// file, so the symbol a change sits inside is knowable only when the
// declaration is in the changed lines, which for a signature it is by
// definition and for a body edit it is not.
func (sh shape) symbolWord(at hunkAt) string {
	touched := sh.touched[at]
	if len(touched) == 0 {
		// Nothing about the declaration changed, so the heading says what the
		// hunk is inside instead, which is still what a reader needs to place
		// it. A change deep in a body has neither, and says nothing.
		if inside := sh.inside[at]; len(inside) > 0 {
			return "  in " + strings.Join(inside, ", ")
		}

		return ""
	}

	parts := make([]string, 0, len(touched))

	for _, s := range touched {
		if mv, ok := sh.moved[declKey(at.path, s)]; ok {
			parts = append(parts, moveWord(at, mv))

			continue
		}

		parts = append(parts, s.Name+" "+s.Kind.String())
	}

	return "  " + strings.Join(parts, " · ")
}

func moveWord(at hunkAt, mv move) string {
	if at == mv.from {
		return mv.name + " moved out"
	}

	return mv.name + " moved in"
}

// fileShape is one file's summary: which symbols the whole file changed and
// how, so a reader can decide what to open before opening any of it.
func (sh shape) fileWord(path string) string {
	// A symbol touched by two hunks is named once, by the strongest thing that
	// happened to it, except a move: the delete and the insert that make one up
	// are the same code and saying "new" of it would be the misreading the
	// structural pass exists to prevent.
	said := map[string]string{}
	rank := map[string]structure.Kind{}

	for at, syms := range sh.touched {
		if at.path != path {
			continue
		}

		for _, s := range syms {
			if _, ok := sh.moved[declKey(path, s)]; ok {
				said[s.Name] = "moved"

				continue
			}

			if was, seenIt := rank[s.Name]; said[s.Name] != "moved" && (!seenIt || s.Kind > was) {
				rank[s.Name], said[s.Name] = s.Kind, s.Kind.String()
			}
		}
	}

	if len(said) == 0 {
		return ""
	}

	names := make([]string, 0, len(said))
	for name := range said {
		names = append(names, name)
	}

	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %s", name, said[name]))
	}

	return strings.Join(parts, " · ")
}
