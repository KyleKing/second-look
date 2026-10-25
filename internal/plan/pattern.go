package plan

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/kyleking/second-look/internal/order"
)

// The thresholds docs/reading-plan.md explains: a seed's files, a shape's share,
// a matched line's letters and digits, and a searched line's length.
const (
	fewestFiles  = 5
	sharePercent = 80
	wordChars    = 4
	beforeChars  = 12
	percent      = 100
)

// Change is one hunk's lines, which is what finding a repeated edit reads.
type Change struct {
	Removed []string
	Added   []string
	Context []string
}

// Pattern is one edit many files make, read once and then file by file for
// what each does beyond it.
type Pattern struct {
	// Shape is the edit as Representative spells it, each line signed + or -.
	Shape []string
	// Representative is the member whose hunks draw the shape: one making it
	// exactly where any does.
	Representative string
	Members        []Member
	// Rest is the chapter's hunks no member's edit is in.
	Rest []order.Ref
	// Before is a line every member carried in the place it edited, which is
	// what finds the files at head that still carry it. Empty where they share none.
	Before string
}

// Member is one file making the edit.
type Member struct {
	Path  string
	Hunks []order.Ref
	// Beyond is what the file changes besides the shape, each line signed + or -.
	Beyond []string
	// Missing counts the shape's lines the file does not make.
	Missing int
}

// Exact reports whether the file makes the shape and nothing else.
func (m Member) Exact() bool { return len(m.Beyond) == 0 && m.Missing == 0 }

// Exactly is how many members make the shape and nothing else.
func (p *Pattern) Exactly() int {
	n := 0

	for _, m := range p.Members {
		if m.Exact() {
			n++
		}
	}

	return n
}

// Dropped is the longest name the shape's removed lines use and its added
// lines do not, which is what finds the files at head still calling it.
func (p *Pattern) Dropped() string {
	const shortest = 4

	kept := map[string]bool{}

	for _, l := range p.Shape {
		if strings.HasPrefix(l, "+") {
			for _, id := range identifier.FindAllString(l, -1) {
				kept[id] = true
			}
		}
	}

	best := ""

	for _, l := range p.Shape {
		if !strings.HasPrefix(l, "-") {
			continue
		}

		for _, id := range identifier.FindAllString(l, -1) {
			if !kept[id] && len(id) >= shortest && (len(id) > len(best) || len(id) == len(best) && id < best) {
				best = id
			}
		}
	}

	return best
}

var (
	identifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	number     = regexp.MustCompile(`\d+`)
	spaces     = regexp.MustCompile(`\s+`)
)

type line struct {
	ref  order.Ref
	raw  string
	norm string
}

type file struct {
	path  string
	lines []line
	set   map[string]bool
}

// stem is the part of a file's name its own identifiers repeat. An index file
// is named by its directory.
func stem(p string) string {
	base := path.Base(p)
	if at := strings.Index(base, "."); at > 0 {
		base = base[:at]
	}

	if slices.Contains([]string{"index", "main", "mod", "init", "__init__"}, base) {
		base = path.Base(path.Dir(p))
	}

	return strings.ToLower(base)
}

// normalize is a line with what differs between files making one edit taken
// out: whitespace, numbers, and the identifiers spelling the file's own name.
func normalize(text, own string) string {
	const shortestStem = 3

	text = spaces.ReplaceAllString(strings.TrimSpace(text), " ")

	if len(own) >= shortestStem {
		text = identifier.ReplaceAllStringFunc(text, func(id string) string {
			if strings.Contains(strings.ToLower(id), own) {
				return "$"
			}

			return id
		})
	}

	return number.ReplaceAllString(text, "#")
}

func meaningful(text string) bool {
	n := 0

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}

	return n >= wordChars
}

func files(refs []order.Ref, changes map[order.Ref]Change) []*file {
	var out []*file

	at := map[string]*file{}

	for _, r := range refs {
		f, ok := at[r.Path]
		if !ok {
			f = &file{path: r.Path, set: map[string]bool{}}
			at[r.Path] = f
			out = append(out, f)
		}

		c := changes[r]
		f.add(r, "-", c.Removed)
		f.add(r, "+", c.Added)
	}

	return out
}

func (f *file) add(r order.Ref, sign string, texts []string) {
	own := stem(f.path)

	for _, text := range texts {
		norm := sign + normalize(text, own)
		if !meaningful(norm) {
			continue
		}

		f.lines = append(f.lines, line{ref: r, raw: sign + text, norm: norm})
		f.set[norm] = true
	}
}

type cluster struct {
	shape   []string
	members []*file
}

// clusters finds every shape at least fewest files make, the most widely made
// first, each file in at most one.
func clusters(pool []*file, fewest int) []cluster {
	var out []cluster

	dead := map[string]bool{}
	left := slices.Clone(pool)

	for {
		seed := widest(left, dead, fewest)
		if seed == "" {
			return out
		}

		making := slices.DeleteFunc(slices.Clone(left), func(f *file) bool { return !f.set[seed] })
		shape := shared(making)
		members := slices.DeleteFunc(making, func(f *file) bool {
			return covered(f, shape)*percent < sharePercent*len(shape)
		})

		if len(members) < fewest {
			dead[seed] = true

			continue
		}

		out = append(out, cluster{shape: shape, members: members})
		left = slices.DeleteFunc(left, func(f *file) bool { return slices.Contains(members, f) })
	}
}

// widest is the line the most files make, the first seen breaking a tie, or
// nothing where no live line reaches fewest files.
func widest(pool []*file, dead map[string]bool, fewest int) string {
	count := map[string]int{}

	var seen []string

	for _, f := range pool {
		for norm := range f.set {
			if !dead[norm] {
				count[norm]++
			}
		}

		for _, l := range f.lines {
			if !dead[l.norm] && !slices.Contains(seen, l.norm) {
				seen = append(seen, l.norm)
			}
		}
	}

	best := ""

	for _, norm := range seen {
		if count[norm] >= fewest && (best == "" || count[norm] > count[best]) {
			best = norm
		}
	}

	return best
}

func shared(making []*file) []string {
	var out []string

	for _, l := range making[0].lines {
		if slices.Contains(out, l.norm) {
			continue
		}

		n := 0

		for _, f := range making {
			if f.set[l.norm] {
				n++
			}
		}

		if n*percent >= sharePercent*len(making) {
			out = append(out, l.norm)
		}
	}

	return out
}

func covered(f *file, shape []string) int {
	n := 0

	for _, norm := range shape {
		if f.set[norm] {
			n++
		}
	}

	return n
}

// member takes the hunks holding a shape line, and nothing else, as the file's.
func member(f *file, shape []string) Member {
	want := map[string]int{}
	for _, norm := range shape {
		want[norm]++
	}

	taken := map[order.Ref]bool{}

	for _, l := range f.lines {
		if want[l.norm] > 0 {
			taken[l.ref] = true
		}
	}

	m := Member{Path: f.path}

	for _, l := range f.lines {
		if !taken[l.ref] {
			continue
		}

		if !slices.Contains(m.Hunks, l.ref) {
			m.Hunks = append(m.Hunks, l.ref)
		}

		if want[l.norm] > 0 {
			want[l.norm]--

			continue
		}

		m.Beyond = append(m.Beyond, l.raw)
	}

	for _, n := range want {
		m.Missing += n
	}

	return m
}

// shapes leaves a hunk a rule reads and a chapter the overlay names to them, and
// puts each shape right behind the first chapter it took hunks from.
func shapes(chapters []Chapter, in Input) []Chapter {
	if len(in.Changes) == 0 {
		return chapters
	}

	ruled := map[order.Ref]bool{}

	for _, h := range in.Hunks {
		_, judged := in.Rules.judge(h)
		ruled[h.Ref] = judged
	}

	named := map[string]bool{}
	for _, e := range in.Overlay.Chapters {
		named[e.Chapter] = true
	}

	names := map[string]bool{}
	for _, c := range chapters {
		names[c.Name] = true
	}

	behind := map[int][]Chapter{}
	taken := map[order.Ref]bool{}

	for _, pool := range pools(chapters, named) {
		for _, cl := range clusters(files(unruled(chapters, pool, ruled), in.Changes), fewestFiles) {
			c := shapeChapter(cl)
			if one := chapters[pool[0]]; one.Source == SourceCommit {
				c.Name = one.Name + " · " + c.Name
				c.Feature = one.Feature
			}

			c.Name = unique(c.Name, names)
			names[c.Name] = true

			for _, r := range c.Hunks {
				taken[r] = true
			}

			first := slices.IndexFunc(pool, func(i int) bool { return slices.Contains(chapters[i].Hunks, c.Hunks[0]) })
			behind[pool[first]] = append(behind[pool[first]], c)
		}
	}

	out := make([]Chapter, 0, len(chapters)+len(behind))

	for i, c := range chapters {
		c.Hunks = slices.DeleteFunc(slices.Clone(c.Hunks), func(r order.Ref) bool { return taken[r] })
		out = append(out, c)
		out = append(out, behind[i]...)
	}

	return out
}

func unruled(chapters []Chapter, pool []int, ruled map[order.Ref]bool) []order.Ref {
	var out []order.Ref

	for _, i := range pool {
		for _, r := range chapters[i].Hunks {
			if !ruled[r] {
				out = append(out, r)
			}
		}
	}

	return out
}

// pools searches each commit alone and every other chapter the overlay does not name together.
func pools(chapters []Chapter, named map[string]bool) [][]int {
	var out [][]int

	var rest []int

	for i, c := range chapters {
		switch {
		case named[c.Name]:
		case c.Source == SourceCommit:
			out = append(out, []int{i})
		default:
			rest = append(rest, i)
		}
	}

	if len(rest) > 0 {
		out = append(out, rest)
	}

	return out
}

func unique(name string, names map[string]bool) string {
	out := name
	for n := 2; names[out]; n++ {
		out = fmt.Sprintf("%s (%d)", name, n)
	}

	return out
}

func shapeChapter(cl cluster) Chapter {
	c := Chapter{
		Name: fmt.Sprintf("the same edit in %d files", len(cl.members)),
		Mode: ModeShape, Source: SourceGraph,
	}

	for _, f := range cl.members {
		c.Hunks = append(c.Hunks, member(f, cl.shape).Hunks...)
	}

	return c
}

// patterns reads every shape-mode chapter's pattern, whatever set the mode.
func patterns(chapters []Chapter, changes map[order.Ref]Change) []string {
	var problems []string

	for i := range chapters {
		c := &chapters[i]
		if c.Mode != ModeShape {
			continue
		}

		c.Pattern = patternOf(c.Hunks, changes)
		if c.Pattern == nil {
			problems = append(problems,
				c.Name+": reads as shape, but no edit repeats across two of its files")
		}
	}

	return problems
}

func patternOf(refs []order.Ref, changes map[order.Ref]Change) *Pattern {
	const fewestToRead = 2

	found := clusters(files(refs, changes), fewestToRead)
	if len(found) == 0 {
		return nil
	}

	cl := found[0]
	p := &Pattern{}
	held := map[order.Ref]bool{}

	for _, f := range cl.members {
		m := member(f, cl.shape)
		p.Members = append(p.Members, m)

		for _, r := range m.Hunks {
			held[r] = true
		}
	}

	rep := representative(p.Members)
	p.Representative = p.Members[rep].Path
	p.Shape = spelled(cl.members[rep], cl.shape)
	p.Before = before(p.Members, changes)

	for _, r := range refs {
		if !held[r] {
			p.Rest = append(p.Rest, r)
		}
	}

	return p
}

// representative is the first exact member, or the one doing least beside the shape.
func representative(members []Member) int {
	best := 0

	for i, m := range members {
		if m.Exact() {
			return i
		}

		if len(m.Beyond)+m.Missing < len(members[best].Beyond)+members[best].Missing {
			best = i
		}
	}

	return best
}

func spelled(f *file, shape []string) []string {
	var out []string

	left := slices.Clone(shape)

	for _, l := range f.lines {
		if at := slices.Index(left, l.norm); at >= 0 {
			out = append(out, l.raw)
			left = slices.Delete(left, at, at+1)
		}
	}

	return out
}

// before prefers a line every member removed over one they all kept around the
// edit, the longest winning and byte order breaking a tie.
func before(members []Member, changes map[order.Ref]Change) string {
	removed := func(c Change) []string { return c.Removed }
	if t := carriedByAll(members, changes, removed); t != "" {
		return t
	}

	return carriedByAll(members, changes, func(c Change) []string { return c.Context })
}

func carriedByAll(members []Member, changes map[order.Ref]Change, side func(Change) []string) string {
	var common map[string]bool

	for _, m := range members {
		carried := map[string]bool{}

		for _, r := range m.Hunks {
			for _, text := range side(changes[r]) {
				if t := strings.TrimSpace(text); len(t) >= beforeChars && meaningful(t) {
					carried[t] = true
				}
			}
		}

		if common == nil {
			common = carried
		}

		maps.DeleteFunc(common, func(t string, _ bool) bool { return !carried[t] })
	}

	return longest(common)
}

func longest(texts map[string]bool) string {
	best := ""

	for t := range texts {
		if len(t) > len(best) || (len(t) == len(best) && t < best) {
			best = t
		}
	}

	return best
}
