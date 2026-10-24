// Package plan orders a change for reading: chapters of hunks, each read in a
// mode and placed by a source the screen can name.
//
// It grows out of order's groups rather than beside them, and keeps their
// invariant: every hunk of the diff lands in exactly one chapter. The layers
// apply cheapest first, and each may move what an earlier one placed: the
// change's own commits, the rules kept per repository, and what the agent and
// the reader wrote into the prepared review.
package plan

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kyleking/second-look/internal/order"
)

// Mode is how a chapter is read.
type Mode string

// The modes a chapter is read in.
const (
	ModeRead  Mode = "read"
	ModeSkim  Mode = "skim"
	ModeShape Mode = "shape"
	ModeTable Mode = "table"
	ModeFold  Mode = "fold"
)

// ErrMode is a mode the plan does not know.
var ErrMode = errors.New("a mode is read, skim, shape, table, or fold")

// Valid reports whether m is one of the modes.
func (m Mode) Valid() bool {
	return slices.Contains([]Mode{ModeRead, ModeSkim, ModeShape, ModeTable, ModeFold}, m)
}

// Source is what placed a chapter or a stop.
type Source string

// The sources a placement records.
const (
	SourceRule   Source = "rule"
	SourceGraph  Source = "graph"
	SourceAgent  Source = "agent"
	SourceYou    Source = "you"
	SourceCommit Source = "commit"
	SourceForge  Source = "forge"
)

// Glyph is the mark a placement is drawn with.
func (s Source) Glyph() string {
	switch s {
	case SourceRule:
		return "§"
	case SourceGraph:
		return "⇢"
	case SourceAgent:
		return "◆"
	case SourceYou:
		return "✎"
	case SourceCommit:
		return "⊙"
	case SourceForge:
		return "⌂"
	}

	return "?"
}

// Plan is the order a change is read in.
type Plan struct {
	Claim    string
	Chapters []Chapter
	Stops    []Stop
	Asks     []Ask
	Features []Feature
	// Basis says what the chapters were cut from, and why the commits were
	// passed over when they were.
	Basis string
	// Problems are placements Build could not honor, each naming what it did instead.
	Problems []string
}

// Chapter is a run of hunks read together.
type Chapter struct {
	Name    string
	Mode    Mode
	Source  Source
	Why     string
	Hunks   []order.Ref
	Feature string
}

// Stop is one place the tour pauses: what the code there is for, and what
// follows from it.
type Stop struct {
	Ref    order.Ref
	Role   string
	What   string
	Then   string
	Source Source
}

// Ask is a question put to the agent about a range, and its answer.
type Ask struct {
	ID        string
	Ref       order.Ref
	Path      string
	StartLine int
	Line      int
	Question  string
	Answer    string
	Source    Source
}

// Feature is a set of chapters one part of the change is read through.
type Feature struct {
	Name     string
	Chapters []int
}

// Commit is one commit of the change and the hunks of the cumulative diff it
// wrote. A hunk two commits wrote is listed under both.
type Commit struct {
	ID      string
	Subject string
	Hunks   []order.Ref
}

// Input is everything a plan is built from.
type Input struct {
	// Hunks is every hunk of the diff, in the diff's order.
	Hunks   []order.Hunk
	Commits []Commit
	// NoCommits says why Commits is empty for a change that has commits, such
	// as a pull request whose commits this checkout does not hold.
	NoCommits string
	Rules     Rules
	Overlay   Overlay
}

// Build applies the layers in turn. Every hunk handed in comes back in exactly
// one chapter, and whatever would have broken that is in Problems.
func Build(in Input) Plan {
	commits, problems := known(in.Commits, in.Hunks)

	chapters, basis := cut(commits, in)
	chapters = in.Rules.apply(chapters, in.Hunks)

	p := Plan{Chapters: chapters, Basis: basis, Problems: problems}
	in.Overlay.apply(&p, in.Hunks)

	var settled []string

	p.Chapters, settled = settle(p.Chapters, in.Hunks)
	p.Problems = append(p.Problems, settled...)
	p.Features = features(p.Chapters)

	return p
}

// cut is the first layer: the commits when they are clean, order's groups otherwise.
func cut(commits []Commit, in Input) ([]Chapter, string) {
	const fallback = "by symbol and directory: "

	if len(commits) == 0 && in.NoCommits != "" {
		return fromGroups(order.Plan(in.Hunks)), fallback + in.NoCommits
	}

	if why := messy(commits, in.Hunks); why != "" {
		return fromGroups(order.Plan(in.Hunks)), fallback + why
	}

	return byCommit(commits, in.Hunks), fmt.Sprintf("by commit: %d commits, in commit order", len(commits))
}

func fromGroups(groups []order.Group) []Chapter {
	out := make([]Chapter, 0, len(groups))

	for _, g := range groups {
		why := "the files in " + g.Name
		if g.Symbol {
			why = "gathered around " + g.Name
		}

		out = append(out, Chapter{
			Name: g.Name, Mode: ModeRead, Source: SourceGraph, Why: why,
			Hunks: slices.Clone(g.Hunks),
		})
	}

	return out
}

// settle holds the invariant: an unknown ref is dropped, a ref placed twice keeps
// its first place, and a hunk nothing placed gets a chapter of its own.
func settle(chapters []Chapter, hunks []order.Hunk) ([]Chapter, []string) {
	known := make(map[order.Ref]bool, len(hunks))
	for _, h := range hunks {
		known[h.Ref] = true
	}

	var (
		problems []string
		out      []Chapter
		placed   = make(map[order.Ref]string, len(hunks))
	)

	for _, c := range chapters {
		kept := c.Hunks[:0:0]

		for _, r := range c.Hunks {
			switch first, twice := placed[r]; {
			case !known[r]:
				problems = append(problems, fmt.Sprintf("%s: %s is not a hunk of this diff", c.Name, name(r)))
			case twice:
				problems = append(problems,
					fmt.Sprintf("%s: %s is already in %s, and stays there", c.Name, name(r), first))
			default:
				placed[r] = c.Name
				kept = append(kept, r)
			}
		}

		if len(kept) > 0 {
			c.Hunks = kept
			out = append(out, c)
		}
	}

	var lost []order.Ref

	for _, h := range hunks {
		if _, ok := placed[h.Ref]; !ok {
			placed[h.Ref] = unplaced
			lost = append(lost, h.Ref)
		}
	}

	if len(lost) > 0 {
		out = append(out, Chapter{
			Name: unplaced, Mode: ModeRead, Source: SourceGraph, Why: "no layer placed these", Hunks: lost,
		})
		problems = append(problems, fmt.Sprintf("%d hunk(s) no layer placed are in %s", len(lost), unplaced))
	}

	return out, problems
}

const unplaced = "unplaced"

func features(chapters []Chapter) []Feature {
	var out []Feature

	for i, c := range chapters {
		if c.Feature == "" {
			continue
		}

		at := slices.IndexFunc(out, func(f Feature) bool { return f.Name == c.Feature })
		if at < 0 {
			out = append(out, Feature{Name: c.Feature})
			at = len(out) - 1
		}

		out[at].Chapters = append(out[at].Chapters, i)
	}

	return out
}

func name(r order.Ref) string { return fmt.Sprintf("%s#%d", r.Path, r.Hunk) }
