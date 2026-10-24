package plan

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/kyleking/second-look/internal/order"
)

// Reasons a rule set is refused.
var (
	ErrNoRepo = errors.New("a plan rule set needs the repository it is for, as owner/name")
	ErrGlob   = errors.New("not a glob")
	ErrAfter  = errors.New("an after rule needs both read and after")
)

// Rules are one repository's reading rules. A glob matches the path from the root,
// ** any number of directories, and a glob with no slash a file name at any depth.
type Rules struct {
	Repo string `toml:"repo"`
	// Fold is read as one line each.
	Fold []string `toml:"fold,omitempty"`
	// NeverFold wins over Fold, a fold mode, and the folding of what a
	// machine wrote.
	NeverFold []string `toml:"never_fold,omitempty"`
	After     []After  `toml:"after,omitempty"`
	// Modes is checked before Fold, the first match winning.
	Modes []ModeRule `toml:"mode,omitempty"`
}

// After moves every chapter touching Read behind the last touching After.
type After struct {
	Read  string `toml:"read"`
	After string `toml:"after"`
}

// ModeRule reads the hunks a glob matches in one mode.
type ModeRule struct {
	Glob string `toml:"glob"`
	Mode Mode   `toml:"mode"`
}

// Validate reports every problem in the set at once.
func (r *Rules) Validate() error {
	var errs []error

	if strings.TrimSpace(r.Repo) == "" {
		errs = append(errs, ErrNoRepo)
	}

	for _, g := range slices.Concat(r.Fold, r.NeverFold) {
		errs = append(errs, checkGlob(g))
	}

	for _, a := range r.After {
		if a.Read == "" || a.After == "" {
			errs = append(errs, fmt.Errorf("%w: %q after %q", ErrAfter, a.Read, a.After))

			continue
		}

		errs = append(errs, checkGlob(a.Read), checkGlob(a.After))
	}

	for _, m := range r.Modes {
		errs = append(errs, checkGlob(m.Glob))

		if !m.Mode.Valid() {
			errs = append(errs, fmt.Errorf("%q: %w, not %q", m.Glob, ErrMode, m.Mode))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("plan rules for %q: %w", r.Repo, err)
	}

	return nil
}

func checkGlob(g string) error {
	if g == "" {
		return fmt.Errorf("%w: an empty pattern", ErrGlob)
	}

	for seg := range strings.SplitSeq(g, "/") {
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Errorf("%w: %q", ErrGlob, g)
		}
	}

	return nil
}

// Match reports whether a repository path is one a glob names.
func Match(glob, name string) bool {
	if !strings.Contains(glob, "/") {
		ok, err := path.Match(glob, path.Base(name))

		return err == nil && ok
	}

	return matchParts(strings.Split(glob, "/"), strings.Split(name, "/"))
}

func matchParts(glob, name []string) bool {
	for len(glob) > 0 {
		if glob[0] == "**" {
			rest := glob[1:]

			for i := range len(name) + 1 {
				if matchParts(rest, name[i:]) {
					return true
				}
			}

			return false
		}

		if len(name) == 0 {
			return false
		}

		if ok, err := path.Match(glob[0], name[0]); err != nil || !ok {
			return false
		}

		glob, name = glob[1:], name[1:]
	}

	return len(name) == 0
}

func matchAny(globs []string, name string) (string, bool) {
	for _, g := range globs {
		if Match(g, name) {
			return g, true
		}
	}

	return "", false
}

// verdict is the mode a rule reads one hunk in, and what to call the rule.
type verdict struct {
	mode   Mode
	label  string
	source Source
}

func (v verdict) why() string {
	if v.source == SourceRule {
		return fmt.Sprintf("%s reads as %s", v.label, v.mode)
	}

	return "written by a machine"
}

func (r *Rules) judge(h order.Hunk) (verdict, bool) {
	never, exempt := matchAny(r.NeverFold, h.Path)

	for _, m := range r.Modes {
		if Match(m.Glob, h.Path) && (m.Mode != ModeFold || !exempt) {
			return verdict{m.Mode, m.Glob, SourceRule}, true
		}
	}

	if exempt {
		return verdict{ModeRead, never, SourceRule}, true
	}

	if g, ok := matchAny(r.Fold, h.Path); ok {
		return verdict{ModeFold, g, SourceRule}, true
	}

	if h.Made {
		return verdict{ModeFold, order.Generated, SourceGraph}, true
	}

	return verdict{}, false
}

// apply is the second layer. A rule covering a whole chapter sets its mode, and one
// covering part splits those hunks into a chapter right behind it.
func (r *Rules) apply(chapters []Chapter, hunks []order.Hunk) []Chapter {
	byRef := make(map[order.Ref]order.Hunk, len(hunks))
	for _, h := range hunks {
		byRef[h.Ref] = h
	}

	out := make([]Chapter, 0, len(chapters))

	for _, c := range chapters {
		out = append(out, r.split(c, byRef)...)
	}

	return r.reorder(out)
}

func (r *Rules) split(c Chapter, byRef map[order.Ref]order.Hunk) []Chapter {
	var (
		stay  []order.Ref
		kinds []verdict
		moved = map[verdict][]order.Ref{}
	)

	for _, ref := range c.Hunks {
		v, ok := r.judge(byRef[ref])
		if !ok || v.mode == c.Mode {
			stay = append(stay, ref)

			continue
		}

		if _, seen := moved[v]; !seen {
			kinds = append(kinds, v)
		}

		moved[v] = append(moved[v], ref)
	}

	if len(kinds) == 0 {
		return []Chapter{c}
	}

	if len(stay) == 0 && len(kinds) == 1 {
		v := kinds[0]
		c.Mode, c.Source, c.Why = v.mode, v.source, v.why()

		return []Chapter{c}
	}

	out := make([]Chapter, 0, len(kinds)+1)
	if len(stay) > 0 {
		kept := c
		kept.Hunks = stay
		out = append(out, kept)
	}

	for _, v := range kinds {
		out = append(out, Chapter{
			Name: c.Name + " · " + v.label, Mode: v.mode, Source: v.source, Why: v.why(),
			Hunks: moved[v], Feature: c.Feature,
		})
	}

	return out
}

// reorder applies each after rule in turn. A chapter touching both sides of
// one stays put, since it cannot be read after itself.
func (r *Rules) reorder(chapters []Chapter) []Chapter {
	for _, a := range r.After {
		chapters = readAfter(chapters, a)
	}

	return chapters
}

func readAfter(chapters []Chapter, a After) []Chapter {
	reads := func(c Chapter) bool { return touches(c, a.Read) && !touches(c, a.After) }

	last := -1

	for i, c := range chapters {
		if touches(c, a.After) && !touches(c, a.Read) {
			last = i
		}
	}

	if last < 0 {
		return chapters
	}

	var moved []Chapter

	out := make([]Chapter, 0, len(chapters))

	for i, c := range chapters {
		if i < last && reads(c) {
			c.Why = strings.TrimPrefix(c.Why+"; read after "+a.After, "; ")
			moved = append(moved, c)

			continue
		}

		out = append(out, c)

		if i == last {
			out = append(out, moved...)
		}
	}

	return out
}

func touches(c Chapter, glob string) bool {
	return slices.ContainsFunc(c.Hunks, func(r order.Ref) bool { return Match(glob, r.Path) })
}
