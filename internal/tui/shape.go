package tui

import (
	"context"
	"fmt"
	"slices"
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
		p := c.Pattern
		if p == nil || p.Before == "" {
			continue
		}

		if _, asked := m.atHead[p.Before]; asked {
			continue
		}

		m.atHead[p.Before] = headHits{}
		ctx, search, needle := m.ctx, m.headSearch, p.Before

		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, headSearchTime)
			defer cancel()

			hits, err := search(ctx, needle)

			return headedMsg{needle: needle, hits: hits, err: err}
		})
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

	rows := []row{label(fmt.Sprintf("THE SHAPE · read once for %s · as %s", plural(len(p.Members), fileNoun),
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

	rows = append(rows, row{kind: rowBlank, comment: noComment}, label(fmt.Sprintf(
		"THE FILES · %d%s · %d exactly the shape, %d do more",
		len(p.Members), in, p.Exactly(), len(p.Members)-p.Exactly(),
	)))

	col := 0
	for _, mem := range p.Members {
		col = max(col, textWidth(strings.TrimPrefix(mem.Path, dir)))
	}

	col = min(col, c.lay.width/half)

	for _, mem := range p.Members {
		rows = append(rows, memberRow(c.d, mem, dir, col))
	}

	rows = append(rows, row{kind: rowBlank, comment: noComment})
	rows = append(rows, headRows(p, c.lay, label)...)

	var more []row

	for _, mem := range p.Members {
		if mem.Path != p.Representative && !mem.Exact() {
			more = append(more, s.fileRows(&c.d.Files[at[mem.Path]], partOf(at, mem.Path, mem.Hunks), c)...)
		}
	}

	for _, path := range refPaths(p.Rest) {
		more = append(more, s.fileRows(&c.d.Files[at[path]], partOf(at, path, p.Rest), c)...)
	}

	if len(more) > 0 {
		rows = append(rows, row{kind: rowBlank, comment: noComment}, label("BEYOND THE SHAPE"))
		rows = append(rows, more...)
	}

	return rows
}

func partOf(at map[string]int, path string, refs []order.Ref) part {
	p := part{file: at[path], path: path, hunks: map[int]bool{}}

	for _, r := range refs {
		if r.Path == path {
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

	what := "exactly the shape"
	if len(mem.Beyond) > 0 {
		first := mem.Beyond[0]
		what = fmt.Sprintf("%s beyond it: %s %s",
			plural(len(mem.Beyond), "line"), first[:1], strings.TrimSpace(first[1:]))
	}

	if mem.Missing > 0 {
		what = strings.TrimPrefix(what+fmt.Sprintf(" · %d of the shape not made", mem.Missing), "exactly the shape · ")
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

func headRows(p *plan.Pattern, lay layout, label func(string) row) []row {
	const head = "NOT CHANGED AT HEAD"

	quoted := "“" + p.Before + "”"

	switch found, asked := lay.atHead[p.Before]; {
	case p.Before == "":
		return []row{label(head + " · the files share no line from before the edit, so nothing was searched")}
	case !lay.searches:
		return []row{label(head + " · no checkout holds head, so nothing was searched")}
	case !asked || !found.done:
		return []row{label(head + " · searching for " + quoted + "…")}
	case found.err != nil:
		return []row{label(head + " · the search failed: " + found.err.Error())}
	default:
		return hitRows(p, found.hits, quoted, label)
	}
}

func hitRows(p *plan.Pattern, hits []blob.Hit, quoted string, label func(string) row) []row {
	const head = "NOT CHANGED AT HEAD"

	held := refPaths(p.Rest)
	for _, mem := range p.Members {
		held = append(held, mem.Path)
	}

	hits = slices.DeleteFunc(slices.Clone(hits), func(h blob.Hit) bool { return slices.Contains(held, h.Path) })
	if len(hits) == 0 {
		return []row{label(head + " · nothing else carries " + quoted)}
	}

	verb := "still carry"
	if len(hits) == 1 {
		verb = "still carries"
	}

	rows := []row{label(fmt.Sprintf("%s · %s %s %s", head, plural(len(hits), fileNoun), verb, quoted))}

	for _, h := range hits[:min(len(hits), headHitsShown)] {
		rows = append(rows, row{
			kind: rowShape, text: fmt.Sprintf("○ %s:%d", h.Path, h.Line), path: h.Path, comment: noComment,
			site: diag.Site{Path: h.Path, Line: h.Line},
		})
	}

	if rest := len(hits) - headHitsShown; rest > 0 {
		rows = append(rows, label(fmt.Sprintf("⋯ %s more", plural(rest, fileNoun))))
	}

	return rows
}

func (m *Model) shapeRow(r row) (string, lipgloss.Style) {
	switch {
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
	out = append(out, fmt.Sprintf("%d exactly the shape, %d do more", p.Exactly(), len(p.Members)-p.Exactly()))

	for i := range rows {
		out = append(out, rows[i].text)
	}

	return out
}
