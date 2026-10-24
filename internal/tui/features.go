package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/order"
)

// featureFiles is the files each feature's chapters touch, in plan order.
func (m *Model) featureFiles() [][]string {
	out := make([][]string, len(m.plan.Features))

	for i := range m.plan.Features {
		out[i] = refPaths(m.featureRefs(i))
	}

	return out
}

// sharers names the other features touching a file.
func (m *Model) sharers(files [][]string, at int, path string) []string {
	var out []string

	for i, fs := range files {
		if i != at && slices.Contains(fs, path) {
			out = append(out, m.plan.Features[i].Name)
		}
	}

	return out
}

func boolRank(b bool) int {
	if b {
		return 1
	}

	return 0
}

func (m *Model) featurePane(size sizes) listPane {
	fs := m.plan.Features
	at := clamp(m.outline.feature, len(fs)-1)
	files := m.featureFiles()

	owned := 0
	for _, f := range fs {
		owned += len(f.Chapters)
	}

	head := " " + m.styles.head.Render(countHead("features", at+1, len(fs)))
	facts := fmt.Sprintf(" · %s in no feature", plural(len(m.plan.Chapters)-owned, chapterNoun))

	return listPane{
		head: head + m.styles.note.Render(facts), n: len(fs), at: at,
		row:    func(i, w int) string { return m.featureRow(size, files, i, w) },
		detail: func(w int) []string { return m.featureDetail(size, files, at, w) },
	}
}

func (m *Model) featureRefs(i int) []order.Ref {
	var out []order.Ref
	for _, c := range m.plan.Features[i].Chapters {
		out = append(out, m.plan.Chapters[c].Hunks...)
	}

	return out
}

func (m *Model) featureRow(size sizes, files [][]string, i, w int) string {
	refs := m.featureRefs(i)
	added, removed := size.of(refs)

	shared := 0
	for _, p := range files[i] {
		if len(m.sharers(files, i, p)) > 0 {
			shared++
		}
	}

	share := ""
	if shared > 0 {
		share = fmt.Sprintf("◇ %d shared", shared)
	}

	tail := right(plural(len(m.plan.Features[i].Chapters), chapterNoun), fileCols+indent) +
		right(plural(len(files[i]), fileNoun), fileCols) +
		right("+"+humanize.Count(added)+" −"+humanize.Count(removed), lineCols) + right(share, lineCols)
	read := m.readAll(refs)
	name := cut(m.plan.Features[i].Name, w-indent-textWidth(tail)-1)
	gap := strings.Repeat(" ", max(1, w-indent-textWidth(name)-textWidth(tail)))
	face := m.styles.body.Render

	if read {
		face = m.styles.behind.Render
	}

	return m.railMark(false, read) + face(name) + gap + m.styles.note.Render(tail)
}

// featureDetail is a feature's chapters and its files, a file another feature
// also touches listed first and marked with the features that do.
func (m *Model) featureDetail(size sizes, files [][]string, at, w int) []string {
	f := m.plan.Features[at]
	room := w - indent

	out := []string{
		" " + m.styles.head.Render(fmt.Sprintf("FEATURE %d OF %d", at+1, len(m.plan.Features))),
		" " + m.styles.file.Render(cut(f.Name, room)),
		"", " " + m.styles.head.Render(fmt.Sprintf("CHAPTERS · %d", len(f.Chapters))),
	}

	for _, c := range f.Chapters {
		ch := m.plan.Chapters[c]
		line := fmt.Sprintf("%d %s %s", c+1, ch.Source.Glyph(), ch.Name)
		out = append(out, " "+m.railMark(false, m.readAll(ch.Hunks))+
			m.styles.body.Render(cut(line, room-indent-modeCols))+m.styles.note.Render("  "+string(ch.Mode)))
	}

	out = append(out, "", " "+m.styles.head.Render(fmt.Sprintf("FILES · %d", len(files[at]))))

	mine := slices.Clone(files[at])
	slices.SortStableFunc(mine, func(a, b string) int {
		return boolRank(len(m.sharers(files, at, b)) > 0) - boolRank(len(m.sharers(files, at, a)) > 0)
	})

	col := 0
	for _, p := range mine {
		col = max(col, textWidth(p))
	}

	col = min(col, room/half)

	for _, p := range mine {
		var refs []order.Ref
		for _, r := range m.featureRefs(at) {
			if r.Path == p {
				refs = append(refs, r)
			}
		}

		added, removed := size.of(refs)
		mark, facts := "  ", fmt.Sprintf("  +%d −%d", added, removed)

		if also := m.sharers(files, at, p); len(also) > 0 {
			mark, facts = "◇ ", facts+"  also "+strings.Join(also, ", ")
		}

		out = append(out, " "+mark+m.styles.body.Render(pad(cutTail(p, col), col))+
			m.styles.note.Render(cut(facts, max(1, room-indent-col))))
	}

	return out
}
