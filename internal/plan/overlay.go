package plan

import (
	"fmt"
	"slices"

	"github.com/kyleking/second-look/internal/order"
)

// Overlay is the last layer: what the agent and the reader wrote into the
// prepared review, resolved against the diff being read.
type Overlay struct {
	Claim    string
	Stops    []Stop
	Chapters []Edit
	Asks     []Ask
	// Problems are entries that no longer find their place in the diff.
	Problems []string
}

// Edit changes one chapter, named as the earlier layers named it. An edit naming
// no chapter starts one where its first hunk was.
type Edit struct {
	Chapter string
	// Name renames the chapter; the rest replace what they set.
	Name    string
	Mode    Mode
	Why     string
	Feature string
	// Hunks move into the chapter from wherever they were.
	Hunks  []order.Ref
	Source Source
}

func (o *Overlay) apply(p *Plan, hunks []order.Hunk) {
	p.Claim = o.Claim
	p.Asks = slices.Clone(o.Asks)
	p.Problems = append(p.Problems, o.Problems...)

	for _, e := range o.Chapters {
		if problem := edit(&p.Chapters, e); problem != "" {
			p.Problems = append(p.Problems, problem)
		}
	}

	in := make(map[order.Ref]bool, len(hunks))
	for _, h := range hunks {
		in[h.Ref] = true
	}

	for _, s := range o.Stops {
		if !in[s.Ref] {
			p.Problems = append(p.Problems, fmt.Sprintf("stop %q: %s is not a hunk of this diff", s.What, name(s.Ref)))

			continue
		}

		p.Stops = append(p.Stops, s)
	}
}

func edit(chapters *[]Chapter, e Edit) string {
	at := slices.IndexFunc(*chapters, func(c Chapter) bool { return c.Name == e.Chapter })
	if at < 0 && len(e.Hunks) == 0 {
		return fmt.Sprintf("chapter %q is not in the plan, and the edit moves nothing into it", e.Chapter)
	}

	from := take(*chapters, e.Hunks)

	if at < 0 {
		at = max(from, 0)
		c := Chapter{Name: e.Chapter, Mode: ModeRead}
		*chapters = slices.Insert(*chapters, at, c)
	}

	c := &(*chapters)[at]
	c.Hunks = append(c.Hunks, e.Hunks...)
	c.Source = e.Source

	if e.Name != "" {
		c.Name = e.Name
	}

	if e.Mode != "" {
		c.Mode = e.Mode
	}

	if e.Why != "" {
		c.Why = e.Why
	}

	if e.Feature != "" {
		c.Feature = e.Feature
	}

	return ""
}

// take removes refs from every chapter, and answers the chapter the first of
// them was in, or -1 when no chapter held it.
func take(chapters []Chapter, refs []order.Ref) int {
	first := -1

	for i := range chapters {
		c := &chapters[i]

		c.Hunks = slices.DeleteFunc(c.Hunks, func(r order.Ref) bool {
			if !slices.Contains(refs, r) {
				return false
			}

			if first < 0 && len(refs) > 0 && r == refs[0] {
				first = i
			}

			return true
		})
	}

	return first
}
