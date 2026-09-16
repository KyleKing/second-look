// Package diag is what a checker says about the lines under review.
//
// A review is read against a diff, and the questions it turns on are often ones
// no diff can answer: whether a field is a member of the type it is read off,
// whether a rule the repository's CI does not run would object. Both are
// answered by a program that already exists on the machine, so this is a
// vocabulary for their answers rather than an implementation of any of them.
//
// Nothing here runs anything. A source produces notes, this places them against
// the diff, and the screen decides what to draw.
package diag

import (
	"cmp"
	"slices"

	"github.com/kyleking/second-look/internal/diff"
)

// Severity is how much a note wants attention. The four are LSP's, because
// every source here is either a language server or a linter that maps onto one.
type Severity uint8

// The severities, strongest first, which is the order a list is read in.
const (
	Error Severity = iota
	Warning
	Info
	Hint
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Info:
		return "info"
	case Hint:
		return "hint"
	}

	return ""
}

// Note is one thing a source has to say about one line.
//
// Line is in the file as it reads after the change, which is the side a review
// comments on and the side a checker was pointed at. A source that cannot say
// where something is does not belong here.
type Note struct {
	Path string
	Line int
	// End is the last line of a note spanning several, and zero for one that
	// does not.
	End int
	// Source names the program that said it, and Code the rule it said it
	// under. Both are shown, because a note nobody can trace to a rule is a
	// note nobody can turn off.
	Source   string
	Code     string
	Message  string
	Severity Severity
}

// Symbol is one name on a line and what a server says it is. It is the answer
// to the other question a diff cannot settle: not what is wrong with a line,
// but what the names on it actually are.
type Symbol struct {
	Name string
	Text string
}

// Placed is notes sorted against the diff: the ones on lines this change wrote,
// and the ones elsewhere in the files it touched.
//
// The split is the whole of the filtering. A checker answers for a file and a
// review is responsible for a change, so a note on a line nobody touched is
// context rather than a finding, and burying the two together would make the
// second pass the first one's noise.
type Placed struct {
	// On is every note anchored to a line the diff carries, by that line.
	On map[Anchor][]Note
	// Elsewhere is every other note in a touched file, by path.
	Elsewhere map[string][]Note
}

// Anchor is one line of one file, which is what a note and a row share.
type Anchor struct {
	Path string
	Line int
}

// Count is how many notes were placed on changed lines, which is the number
// worth putting in front of a reader.
func (p Placed) Count() int {
	n := 0
	for _, ns := range p.On {
		n += len(ns)
	}

	return n
}

// Place sorts notes against the diff. A note on a file the diff does not carry
// is dropped: a checker pointed at a project answers for the whole of it, and a
// review is not the place to read the rest.
func Place(d *diff.Diff, notes []Note) Placed {
	out := Placed{On: map[Anchor][]Note{}, Elsewhere: map[string][]Note{}}
	carried, changed := coverage(d)

	for _, n := range notes {
		if !carried[n.Path] {
			continue
		}

		if at := (Anchor{n.Path, n.Line}); changed[at] {
			out.On[at] = append(out.On[at], n)

			continue
		}

		out.Elsewhere[n.Path] = append(out.Elsewhere[n.Path], n)
	}

	for at := range out.On {
		sortNotes(out.On[at])
	}

	for path := range out.Elsewhere {
		sortNotes(out.Elsewhere[path])
	}

	return out
}

// coverage is which files the diff carries and which of their after-side lines
// it wrote. A context line is carried and not changed: a note on it is about
// code this change did not write.
func coverage(d *diff.Diff) (map[string]bool, map[Anchor]bool) {
	files, lines := map[string]bool{}, map[Anchor]bool{}

	for i := range d.Files {
		f := &d.Files[i]
		files[f.NewPath] = true

		for _, l := range f.Lines {
			if l.Kind == '+' && l.New > 0 {
				lines[Anchor{f.NewPath, l.New}] = true
			}
		}
	}

	return files, lines
}

// sortNotes puts the strongest first and ties in a fixed order, so two runs
// over the same file list the same notes the same way.
func sortNotes(ns []Note) {
	slices.SortStableFunc(ns, func(a, b Note) int {
		return cmp.Or(
			cmp.Compare(a.Severity, b.Severity),
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Code, b.Code),
			cmp.Compare(a.Message, b.Message),
		)
	})
}

// Files is every path the diff's after side carries, which is what a source is
// asked to look at.
func Files(d *diff.Diff) []string {
	out := make([]string, 0, len(d.Files))

	for i := range d.Files {
		// A deleted file has no after side, and a binary one has no lines a
		// checker could answer for.
		if f := &d.Files[i]; f.NewPath != "" && f.NewPath != "/dev/null" && len(f.Lines) > 0 {
			out = append(out, f.NewPath)
		}
	}

	return out
}
