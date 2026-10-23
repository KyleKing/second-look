package tui

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
)

const headerRows = 2

const titleFloor = 24

const (
	rankKept = iota
	rankProblem
	rankAgent
	rankCounts
	rankRead
	rankAuthor
	rankCost
)

type fact struct {
	text string
	rank int
}

func (m *Model) title() string {
	name := m.styles.title.Render(m.name())
	facts := m.facts()
	about := m.about.Title

	for limit := rankCost; ; limit-- {
		right := joinFacts(facts, limit)
		room := m.width - textWidth(m.name()) - textWidth(right) - indent - 1
		want := min(textWidth(about), titleFloor)

		if limit >= rankKept && room < want {
			continue
		}

		left := name
		if room >= len(shortestTitle) && about != "" {
			left += "  " + m.styles.body.Render(cut(about, room))
		}

		gap := max(1, m.width-textWidth(left)-textWidth(right))

		return cut(left+strings.Repeat(" ", gap)+m.styles.subtitle.Render(right), m.width)
	}
}

const shortestTitle = "        "

func joinFacts(facts []fact, limit int) string {
	var out []string

	for _, f := range facts {
		if f.rank <= limit {
			out = append(out, f.text)
		}
	}

	return strings.Join(out, " · ")
}

func (m *Model) facts() []fact {
	var out []fact

	add := func(rank int, text string) {
		if text != "" {
			out = append(out, fact{text: text, rank: rank})
		}
	}

	add(rankAuthor, m.authorWord())
	add(rankCounts, strings.Join(append(m.counts().words(), m.agentNotes()...), " · "))
	add(rankRead, m.readCount())

	if m.newHead != "" {
		add(rankKept, "head moved to "+short(m.newHead))
	}

	if word := m.treeWord(); word != "" {
		add(rankKept, m.styles.warn.Render(word))
	}

	if m.verifying {
		add(rankProblem, m.styles.note.Render("checking head…"))
	}

	add(rankAgent, m.agentWord())

	if word := m.troubleWord(); word != "" {
		add(rankProblem, m.styles.warn.Render(word))
	}

	if m.cost.Rated() {
		add(rankCost, fmt.Sprintf("cost %d", m.cost.Total))
	}

	add(rankKept, m.position())

	return out
}

func (m *Model) authorWord() string {
	added, removed := 0, 0

	for i := range m.diff.Files {
		for _, l := range m.diff.Files[i].Lines {
			switch l.Kind {
			case diff.KindAdd:
				added++
			case diff.KindRemove:
				removed++
			}
		}
	}

	parts := make([]string, 0, 3)

	if m.about.Author != "" {
		parts = append(parts, m.about.Author)
	}

	return strings.Join(append(parts, plural(len(m.diff.Files), "file"),
		fmt.Sprintf("+%d −%d", added, removed)), " · ")
}

func (m *Model) agentNotes() []string {
	n := 0

	for i := range m.review.Comments {
		if agentOrigin(m.review.Comments[i].Origin) {
			n++
		}
	}

	if n == 0 {
		return nil
	}

	return []string{fmt.Sprintf("%s %d agent", agentGlyph, n)}
}

func (m *Model) modes() string {
	words := make([]string, 0, 5)

	for _, w := range []string{m.view.String(), m.drawnWord(), m.orderWord(), m.fileWord(), m.groupWord()} {
		if w = strings.TrimSpace(w); w != "" {
			words = append(words, w)
		}
	}

	right := strings.Join(words, " · ")
	path := fitPath(m.rowPath(), m.width-textWidth(right)-indent-1)

	gap := max(1, m.width-textWidth(path)-textWidth(right))

	return cut(m.styles.file.Render(path)+strings.Repeat(" ", gap)+m.styles.hunk.Render(right), m.width)
}

func (m *Model) drawnWord() string {
	if m.drawn == opening {
		return ""
	}

	return m.drawn.String()
}
