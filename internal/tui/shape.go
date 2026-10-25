package tui

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/seen"
)

// HeadSearch finds the files at head carrying a line verbatim.
type HeadSearch func(ctx context.Context, text string) ([]blob.Hit, error)

// WithHeadSearch lets a shape chapter name the files at head the edit missed.
// Without one the section says nothing could be searched.
func WithHeadSearch(search HeadSearch) Option {
	return func(m *Model) { m.headSearch = search }
}

// HeadList is every file at head under a directory.
type HeadList func(ctx context.Context, dir string) ([]string, error)

// WithHeadList lets a shape chapter name the files at head named like its own.
func WithHeadList(list HeadList) Option {
	return func(m *Model) { m.headList = list }
}

// A lookup is one way of finding a file at head the edit missed, keyed so each
// is asked once: by a line the files carried, a name they stopped calling, or a
// file name like theirs.
type lookup struct {
	key, why string
}

const namedPrefix = "named:"

func lookups(p *plan.Pattern) []lookup {
	var out []lookup

	if p.Before != "" {
		out = append(out, lookup{p.Before, "carries “" + p.Before + "”"})
	}

	if name := p.Dropped(); name != "" && name != p.Before {
		out = append(out, lookup{name, "calls " + name})
	}

	if glob := nameGlob(p.Members); glob != "" {
		out = append(out, lookup{namedPrefix + glob, "named like them"})
	}

	return out
}

// nameGlob is the members' shared directory, a * for each directory below it
// they sit in, and the start and end their file names share, or nothing where
// the names share no extension or the members sit at different depths.
func nameGlob(members []plan.Member) string {
	names := make([]string, len(members))
	depth := strings.Count(members[0].Path, "/")

	for i, mem := range members {
		names[i] = path.Base(mem.Path)
		if strings.Count(mem.Path, "/") != depth {
			return ""
		}
	}

	dir := sharedDir(members)
	dir += strings.Repeat("*/", depth-strings.Count(dir, "/"))

	start, end := names[0], names[0]

	for _, n := range names[1:] {
		for !strings.HasPrefix(n, start) {
			start = start[:len(start)-1]
		}

		for !strings.HasSuffix(n, end) {
			end = end[1:]
		}
	}

	if !strings.Contains(end, ".") {
		return ""
	}

	if start == names[0] {
		return dir + start
	}

	return dir + start + "*" + end
}

// look runs one lookup against the head.
func (m *Model) look(ctx context.Context, key string) ([]blob.Hit, error) {
	glob, named := strings.CutPrefix(key, namedPrefix)
	if !named {
		return m.headSearch(ctx, key)
	}

	if m.headList == nil {
		return nil, nil
	}

	dir, _, _ := strings.Cut(glob, "*")

	files, err := m.headList(ctx, dir[:strings.LastIndex(dir, "/")+1])
	if err != nil {
		return nil, err
	}

	var out []blob.Hit

	for _, f := range files {
		ok, err := path.Match(glob, f)
		if err != nil {
			return nil, fmt.Errorf("matching %s: %w", glob, err)
		}

		if ok {
			out = append(out, blob.Hit{Path: f})
		}
	}

	return out, nil
}

const (
	headSearchTime = 10 * time.Second
	headHitsShown  = 20
)

type headHits struct {
	hits []blob.Hit
	err  error
	done bool
}

type headedMsg struct {
	needle string
	hits   []blob.Hit
	err    error
}

func hunkChanges(d *diff.Diff) map[order.Ref]plan.Change {
	out := map[order.Ref]plan.Change{}

	for i := range d.Files {
		p := filePath(&d.Files[i])

		for _, l := range d.Files[i].Lines {
			at := order.Ref{Path: p, Hunk: l.Hunk}
			c := out[at]

			switch l.Kind {
			case diff.KindAdd:
				c.Added = append(c.Added, l.Text)
			case diff.KindRemove:
				c.Removed = append(c.Removed, l.Text)
			default:
				c.Context = append(c.Context, l.Text)
			}

			out[at] = c
		}
	}

	return out
}

// askHead searches the head for each shape's line from before the edit, once
// per line and behind the first frame.
func (m *Model) askHead() tea.Cmd {
	if m.headSearch == nil {
		return nil
	}

	var cmds []tea.Cmd

	for _, c := range m.plan.Chapters {
		if c.Pattern == nil {
			continue
		}

		for _, l := range lookups(c.Pattern) {
			if _, asked := m.atHead[l.key]; asked {
				continue
			}

			m.atHead[l.key] = headHits{}
			ctx, needle := m.ctx, l.key

			cmds = append(cmds, func() tea.Msg {
				ctx, cancel := context.WithTimeout(ctx, headSearchTime)
				defer cancel()

				hits, err := m.look(ctx, needle)

				return headedMsg{needle: needle, hits: hits, err: err}
			})
		}
	}

	return tea.Batch(cmds...)
}

func (m *Model) applyHeaded(msg headedMsg) {
	m.atHead[msg.needle] = headHits{hits: msg.hits, err: msg.err, done: true}
	m.rebuild()
}

func (l layout) patternOf(g planGroup) *plan.Pattern {
	if g.chapter == 0 || g.mode != plan.ModeShape {
		return nil
	}

	return l.plan[g.chapter-1].Pattern
}

// shapeRows draws every member doing more than the shape in full, and the exact ones only as rows.
func (s screen) shapeRows(p *plan.Pattern, c fileCtx) []row {
	at := fileIndex(c.d)
	label := func(text string) row { return row{kind: rowShape, head: true, text: text, comment: noComment} }

	rows := []row{label(fmt.Sprintf("SHAPE · %s · %s", plural(len(p.Members), fileNoun),
		p.Representative))}

	var rep plan.Member

	for _, mem := range p.Members {
		if mem.Path == p.Representative {
			rep = mem
		}
	}

	rows = append(rows, s.fileRows(&c.d.Files[at[rep.Path]], partOf(at, rep.Path, rep.Hunks), c)...)

	dir := sharedDir(p.Members)
	in := ""

	if dir != "" {
		in = " in " + dir
	}

	files := label(fmt.Sprintf("FILES · %d%s · %d exact, %d more",
		len(p.Members), in, p.Exactly(), len(p.Members)-p.Exactly()))
	files.refs = exactRefs(p)
	rows = append(rows, row{kind: rowBlank, comment: noComment}, files)

	col := 0
	for _, mem := range p.Members {
		col = max(col, textWidth(strings.TrimPrefix(mem.Path, dir)))
	}

	col = min(col, c.lay.width/half)

	for _, mem := range p.Members {
		rows = append(rows, memberRow(c.d, mem, dir, col))

		if mem.Exact() && c.lay.fold.members[mem.Path] {
			rows = append(rows, s.fileRows(&c.d.Files[at[mem.Path]], partOf(at, mem.Path, mem.Hunks), c)...)
		}
	}

	rows = append(rows, row{kind: rowBlank, comment: noComment})
	rows = append(rows, headRows(p, c.lay, label)...)

	var more []row

	for _, mem := range p.Members {
		if mem.Path != p.Representative && !mem.Exact() {
			drawn := s.fileRows(&c.d.Files[at[mem.Path]], partOf(at, mem.Path, mem.Hunks), c)
			more = append(more, sharing(drawn, mem)...)
		}
	}

	for _, name := range refPaths(p.Rest) {
		more = append(more, s.fileRows(&c.d.Files[at[name]], partOf(at, name, p.Rest), c)...)
	}

	if len(more) > 0 {
		rows = append(rows, row{kind: rowBlank, comment: noComment}, label("BEYOND THE SHAPE"))
		rows = append(rows, more...)
	}

	return rows
}

func isMember(r row) bool { return r.kind == rowShape && !r.head && len(r.refs) > 0 }

// exactMember reports whether a shape's file makes the shape and nothing else.
func (m *Model) exactMember(name string) bool {
	for _, c := range m.plan.Chapters {
		if c.Pattern == nil {
			continue
		}

		for _, mem := range c.Pattern.Members {
			if mem.Path == name {
				return mem.Exact()
			}
		}
	}

	return false
}

// exactRefs is every hunk of the members making the shape and nothing else.
func exactRefs(p *plan.Pattern) []order.Ref {
	var out []order.Ref

	for _, mem := range p.Members {
		if mem.Exact() {
			out = append(out, mem.Hunks...)
		}
	}

	return out
}

// sharing marks the changed lines a member makes as the shape does, which is
// every one its Beyond does not name, so what it does besides stands out.
func sharing(rows []row, mem plan.Member) []row {
	beyond := map[string]int{}
	for _, l := range mem.Beyond {
		beyond[l]++
	}

	for i := range rows {
		l := rows[i].line
		if rows[i].path != mem.Path || (l.Kind != diff.KindAdd && l.Kind != diff.KindRemove) {
			continue
		}

		signed := string(l.Kind) + l.Text
		if beyond[signed] > 0 {
			beyond[signed]--

			continue
		}

		rows[i].shared = true
	}

	return rows
}

func partOf(at map[string]int, name string, refs []order.Ref) part {
	p := part{file: at[name], path: name, hunks: map[int]bool{}}

	for _, r := range refs {
		if r.Path == name {
			p.hunks[r.Hunk] = true
		}
	}

	return p
}

// sharedDir ends in a slash, so trimming it leaves a path relative to it.
func sharedDir(members []plan.Member) string {
	dir := dirOf(members[0].Path) + "/"

	for _, mem := range members {
		for !strings.HasPrefix(mem.Path, dir) {
			at := strings.LastIndex(strings.TrimSuffix(dir, "/"), "/")
			if at < 0 {
				return ""
			}

			dir = dir[:at+1]
		}
	}

	return dir
}

func memberRow(d *diff.Diff, mem plan.Member, dir string, col int) row {
	added, removed := 0, 0
	for _, r := range mem.Hunks {
		a, rm := d.Changed(r.Path, r.Hunk)
		added, removed = added+a, removed+rm
	}

	what := "exact"
	if len(mem.Beyond) > 0 {
		first := mem.Beyond[0]
		what = fmt.Sprintf("%s beyond: %s %s",
			plural(len(mem.Beyond), "line"), first[:1], strings.TrimSpace(first[1:]))
	}

	if mem.Missing > 0 {
		what = strings.TrimPrefix(what+fmt.Sprintf(" · %d missing", mem.Missing), "exact · ")
	}

	text := fmt.Sprintf("%s  %s  +%d −%d  %s", pad(cutTail(strings.TrimPrefix(mem.Path, dir), col), col),
		plural(len(mem.Hunks), "hunk"), added, removed, what)

	return row{
		kind: rowShape, text: text, path: mem.Path, comment: noComment, refs: mem.Hunks,
		site: diag.Site{Path: mem.Path, Line: firstNew(d, mem.Hunks[0])},
	}
}

// firstNew is the first line a hunk changes at head, or its first line where
// it only removes.
func firstNew(d *diff.Diff, r order.Ref) int {
	first := 0

	for i := range d.Files {
		if filePath(&d.Files[i]) != r.Path {
			continue
		}

		for _, l := range d.Files[i].Lines {
			switch {
			case l.Hunk != r.Hunk || l.New == 0:
			case l.Kind == diff.KindAdd:
				return l.New
			case first == 0:
				first = l.New
			}
		}
	}

	return first
}

const notChanged = "NOT CHANGED AT HEAD"

func headRows(p *plan.Pattern, lay layout, label func(string) row) []row {
	looks := lookups(p)

	if len(looks) == 0 {
		return []row{label(notChanged + " · nothing to search for")}
	}

	if !lay.searches {
		return []row{label(notChanged + " · no checkout")}
	}

	for _, l := range looks {
		switch found, asked := lay.atHead[l.key]; {
		case !asked || !found.done:
			return []row{label(notChanged + " · searching…")}
		case found.err != nil:
			return []row{label(notChanged + " · search failed: " + found.err.Error())}
		}
	}

	return hitRows(p, looks, lay.atHead, label)
}

// missed is a file at head a lookup found, with why it was found.
type missed struct {
	hit blob.Hit
	why string
}

func hitRows(p *plan.Pattern, looks []lookup, found map[string]headHits, label func(string) row) []row {
	held := refPaths(p.Rest)
	for _, mem := range p.Members {
		held = append(held, mem.Path)
	}

	var hits []missed

	for _, l := range looks {
		for _, h := range found[l.key].hits {
			if !slices.Contains(held, h.Path) {
				held = append(held, h.Path)
				hits = append(hits, missed{h, l.why})
			}
		}
	}

	if len(hits) == 0 {
		return []row{label(notChanged + " · nothing like these")}
	}

	rows := []row{label(fmt.Sprintf("%s · %s like these", notChanged,
		plural(len(hits), fileNoun)))}

	for _, h := range hits[:min(len(hits), headHitsShown)] {
		where := h.hit.Path
		if h.hit.Line > 0 {
			where += ":" + strconv.Itoa(h.hit.Line)
		}

		rows = append(rows, row{
			kind: rowShape, text: "○ " + where + " · " + h.why, path: h.hit.Path, comment: noComment,
			site: diag.Site{Path: h.hit.Path, Line: max(1, h.hit.Line)},
		})
	}

	if rest := len(hits) - headHitsShown; rest > 0 {
		rows = append(rows, label(fmt.Sprintf("⋯ %s more", plural(rest, fileNoun))))
	}

	return rows
}

func (m *Model) shapeRow(r row) (string, lipgloss.Style) {
	switch {
	case r.head && len(r.refs) > 0:
		return "    " + r.text + "  [space] read", m.styles.head
	case r.head:
		return "    " + r.text, m.styles.head
	case len(r.refs) > 0:
		mark := "  "
		if m.readAll(r.refs) {
			mark = "✓ "
		}

		return "    " + mark + r.text, m.styles.body
	}

	return "    " + r.text, m.styles.note
}

func (m *Model) seenRefs(refs []order.Ref) []seen.Ref {
	out := make([]seen.Ref, 0, len(refs))
	for _, r := range refs {
		out = append(out, seen.Ref{Path: r.Path, Hunk: r.Hunk, ID: seen.Hunk(m.diff, r.Path, r.Hunk)})
	}

	return out
}

func (m *Model) shapeWords(p *plan.Pattern) []string {
	label := func(text string) row { return row{text: text} }
	rows := headRows(p, layout{atHead: m.atHead, searches: m.headSearch != nil}, label)

	out := make([]string, 0, len(rows)+1)
	out = append(out, fmt.Sprintf("%d exact, %d more", p.Exactly(), len(p.Members)-p.Exactly()))

	for i := range rows {
		out = append(out, rows[i].text)
	}

	return out
}
