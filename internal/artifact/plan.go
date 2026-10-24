package artifact

import (
	"errors"
	"fmt"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
)

// Plan is what the agent and you changed about the reading order. It is local, and
// each entry is anchored the way a comment is, so it survives a head that moved.
type Plan struct {
	Claim    string        `json:"claim,omitempty"    toml:"claim,omitempty"`
	Stops    []PlanStop    `json:"stops,omitempty"    toml:"stop,omitempty"`
	Chapters []PlanChapter `json:"chapters,omitempty" toml:"chapter,omitempty"`
	Asks     []PlanAsk     `json:"asks,omitempty"     toml:"ask,omitempty"`
}

// Span is a range of one side of the diff. Anchor quotes its last line, the way
// a comment's does, and is recorded when the entry is staged.
type Span struct {
	Path      string `json:"path"                 toml:"path"`
	Side      string `json:"side"                 toml:"side"`
	StartLine int    `json:"start_line,omitempty" toml:"start_line,omitempty"`
	Line      int    `json:"line"                 toml:"line"`
	Anchor    string `json:"anchor,omitempty"     toml:"anchor,omitempty"`
}

// PlanStop is one place the tour pauses.
type PlanStop struct {
	ID string `json:"id" toml:"id"`
	Span
	Role   string `json:"role,omitempty"   toml:"role,omitempty"`
	What   string `json:"what"             toml:"what"`
	Then   string `json:"then,omitempty"   toml:"then,omitempty"`
	Origin string `json:"origin,omitempty" toml:"origin,omitempty"`
}

// PlanChapter changes the chapter the earlier layers named Chapter, or starts one
// by that name around the hunks it moves.
type PlanChapter struct {
	Chapter string    `json:"chapter"           toml:"chapter"`
	Name    string    `json:"name,omitempty"    toml:"name,omitempty"`
	Mode    plan.Mode `json:"mode,omitempty"    toml:"mode,omitempty"`
	Why     string    `json:"why,omitempty"     toml:"why,omitempty"`
	Feature string    `json:"feature,omitempty" toml:"feature,omitempty"`
	// Hunks names each hunk moved into the chapter by a line inside it.
	Hunks  []Span `json:"hunks,omitempty"  toml:"hunk,omitempty"`
	Origin string `json:"origin,omitempty" toml:"origin,omitempty"`
}

// PlanAsk is a question put to the agent about a range, and its answer.
type PlanAsk struct {
	ID string `json:"id" toml:"id"`
	Span
	Question string `json:"question"         toml:"question"`
	Answer   string `json:"answer,omitempty" toml:"answer,omitempty"`
	Origin   string `json:"origin,omitempty" toml:"origin,omitempty"`
	// Outside marks a line the diff does not carry, which is placed by its path and line alone.
	Outside bool `json:"outside,omitempty" toml:"outside,omitempty"`
}

// Reasons a plan entry is refused.
var (
	ErrNoID      = errors.New("id is required")
	ErrNoWhat    = errors.New("a stop says what the code there does")
	ErrNoChapter = errors.New("chapter names the chapter the edit is to")
	ErrNoAsk     = errors.New("an ask carries its question")
)

func (p *Plan) validate() []error {
	var errs []error

	ids := map[string]bool{}
	unique := func(kind, id string) {
		if ids[kind+id] {
			errs = append(errs, fmt.Errorf("plan %s %s: %w", kind, id, ErrDuplicateID))
		}

		ids[kind+id] = true
	}

	for i := range p.Stops {
		s := &p.Stops[i]
		where := fmt.Sprintf("plan stop %d", i+1)
		errs = append(errs, s.check(where)...)
		errs = append(errs, required(where, s.ID, ErrNoID), required(where, s.What, ErrNoWhat))
		unique("stop", s.ID)
	}

	for i, c := range p.Chapters {
		where := fmt.Sprintf("plan chapter %d", i+1)
		errs = append(errs, required(where, c.Chapter, ErrNoChapter))

		if c.Mode != "" && !c.Mode.Valid() {
			errs = append(errs, fmt.Errorf("%s: %w, not %q", where, plan.ErrMode, c.Mode))
		}

		for _, h := range c.Hunks {
			errs = append(errs, h.check(where)...)
		}
	}

	for i := range p.Asks {
		a := &p.Asks[i]
		where := fmt.Sprintf("plan ask %d", i+1)
		errs = append(errs, a.check(where)...)
		errs = append(errs, required(where, a.ID, ErrNoID), required(where, a.Question, ErrNoAsk))
		unique("ask", a.ID)
	}

	return errs
}

func required(where, value string, missing error) error {
	if value == "" {
		return fmt.Errorf("%s: %w", where, missing)
	}

	return nil
}

func (s *Span) check(where string) []error {
	var errs []error

	if s.Path == "" {
		errs = append(errs, fmt.Errorf("%s: %w", where, ErrNoPath))
	}

	if s.Line <= 0 {
		errs = append(errs, fmt.Errorf("%s: %w", where, ErrLine))
	}

	if !validSide[s.Side] {
		errs = append(errs, fmt.Errorf("%s: %w: %q", where, ErrSide, s.Side))
	}

	if s.StartLine > s.Line {
		errs = append(errs, fmt.Errorf("%s: %w: %d is after %d", where, ErrStartLine, s.StartLine, s.Line))
	}

	return errs
}

// spans is every span the plan carries, for the passes that treat them alike.
func (p *Plan) spans() []*Span {
	var out []*Span

	for i := range p.Stops {
		out = append(out, &p.Stops[i].Span)
	}

	for i := range p.Chapters {
		for j := range p.Chapters[i].Hunks {
			out = append(out, &p.Chapters[i].Hunks[j])
		}
	}

	for i := range p.Asks {
		if !p.Asks[i].Outside {
			out = append(out, &p.Asks[i].Span)
		}
	}

	return out
}

// ResolvePlan quotes the line each span ends on, refusing a span the diff does not
// carry, the way Resolve does for a comment.
func ResolvePlan(p *Plan, d *diff.Diff) error {
	if err := usable(d); err != nil {
		return err
	}

	var errs []error

	for _, s := range p.spans() {
		text, ok := d.Anchor(s.Path, s.Side, s.Line)
		if !ok {
			errs = append(errs, fmt.Errorf("plan: %w: %s %s line %d", ErrAnchorMissing, s.Path, s.Side, s.Line))

			continue
		}

		if s.StartLine != 0 {
			if _, ok := d.Anchor(s.Path, s.Side, s.StartLine); !ok {
				errs = append(errs, fmt.Errorf("plan: %w: %s %s start_line %d",
					ErrAnchorMissing, s.Path, s.Side, s.StartLine))

				continue
			}
		}

		s.Anchor = text
	}

	return errors.Join(errs...)
}

// Overlay is the plan resolved against d for the reading plan's last layer. An entry
// whose anchor no longer finds its line is reported rather than placed.
func (p *Plan) Overlay(d *diff.Diff) plan.Overlay {
	out := plan.Overlay{Claim: p.Claim}

	place := func(what string, s Span) (Span, order.Ref, bool) {
		ref, err := s.locate(d)
		if err != nil {
			out.Problems = append(out.Problems, fmt.Sprintf("%s: %v", what, err))

			return s, order.Ref{}, false
		}

		s.StartLine += ref.line - s.Line
		s.Line = ref.line

		return s, ref.Ref, true
	}

	for i := range p.Stops {
		s := &p.Stops[i]
		if _, ref, ok := place("stop "+s.ID, s.Span); ok {
			out.Stops = append(out.Stops, plan.Stop{
				Ref: ref, Role: s.Role, What: s.What, Then: s.Then, Source: sourceOf(s.Origin),
			})
		}
	}

	for _, c := range p.Chapters {
		e := plan.Edit{
			Chapter: c.Chapter, Name: c.Name, Mode: c.Mode, Why: c.Why, Feature: c.Feature,
			Source: sourceOf(c.Origin),
		}

		for _, h := range c.Hunks {
			if _, ref, ok := place("chapter "+c.Chapter, h); ok {
				e.Hunks = append(e.Hunks, ref)
			}
		}

		out.Chapters = append(out.Chapters, e)
	}

	for i := range p.Asks {
		a := &p.Asks[i]
		if a.Outside {
			out.Asks = append(out.Asks, plan.Ask{
				ID: a.ID, Path: a.Path, Side: a.Side, StartLine: a.StartLine, Line: a.Line,
				Question: a.Question, Answer: a.Answer, Source: sourceOf(a.Origin),
			})

			continue
		}

		if s, ref, ok := place("ask "+a.ID, a.Span); ok {
			out.Asks = append(out.Asks, plan.Ask{
				ID: a.ID, Ref: ref, Path: s.Path, Side: s.Side, StartLine: s.StartLine, Line: s.Line,
				Question: a.Question, Answer: a.Answer, Source: sourceOf(a.Origin),
			})
		}
	}

	return out
}

type located struct {
	order.Ref

	line int
}

// locate is the hunk a span's anchor stands in now, and the line it stands on.
func (s *Span) locate(d *diff.Diff) (located, error) {
	line, err := locate(d, s.Path, s.Side, s.Line, s.Anchor)
	if err != nil {
		return located{}, err
	}

	hunk, _ := d.HunkOf(s.Path, s.Side, line)

	path := s.Path

	for i := range d.Files {
		f := &d.Files[i]
		if s.Side == SideLeft && f.OldPath == s.Path && f.NewPath != "" {
			path = f.NewPath
		}
	}

	return located{Ref: order.Ref{Path: path, Hunk: hunk}, line: line}, nil
}

func sourceOf(origin string) plan.Source {
	if origin == OriginMe {
		return plan.SourceYou
	}

	return plan.SourceAgent
}
