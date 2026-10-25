package tui

import (
	"fmt"
	"strings"
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

func (m *Model) title(files []fileStat) string {
	name := m.styles.title.Render(m.name())
	facts := m.facts(files)
	about := m.about.Title

	for limit := rankCost; ; limit-- {
		right := joinFacts(facts, limit)
		room := m.width - textWidth(m.name()) - textWidth(right) - indent - 1
		want := min(textWidth(about), titleFloor)

		// A problem outranks the title, so the title gives way before a problem is dropped.
		if limit > rankProblem && room < want {
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

func (m *Model) facts(files []fileStat) []fact {
	var out []fact

	add := func(rank int, text string) {
		if text != "" {
			out = append(out, fact{text: text, rank: rank})
		}
	}

	add(rankAuthor, m.authorWord(files))
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

	add(rankAgent, m.judgeWord())
	add(rankAgent, m.agentWord())

	if word := m.troubleWord(); word != "" {
		add(rankProblem, m.styles.warn.Render(word))
	}

	if n := len(m.plan.Problems); n > 0 {
		add(rankProblem, m.styles.warn.Render(plural(n, "plan problem")+", i says"))
	}

	if m.cost.Rated() {
		add(rankCost, fmt.Sprintf("cost %d", m.cost.Total))
	}

	add(rankKept, m.position())

	return out
}

func (m *Model) authorWord(files []fileStat) string {
	added, removed := 0, 0

	for _, f := range files {
		added += f.added
		removed += f.removed
	}

	parts := make([]string, 0, 3)

	if m.about.Author != "" {
		parts = append(parts, m.about.Author)
	}

	return strings.Join(append(parts, plural(len(files), "file"),
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

func (m *Model) modes(files []fileStat) string {
	sep := m.styles.hunk.Render(" · ")
	words := make([]string, 0, 6)

	for _, w := range []string{
		m.outlineWord(), m.view.String(), m.outsideWord(), m.drawnWord(), m.orderWord(), m.fileWord(),
	} {
		if w = strings.TrimSpace(w); w != "" {
			words = append(words, m.styles.hunk.Render(w))
		}
	}

	right := strings.Join(words, sep)
	room := m.width - textWidth(right) - indent

	if room < 1 {
		return cut(right, m.width)
	}

	line := m.strip(files, room)
	if m.touring() {
		line = m.tourStrip(room)
	}

	return line + strings.Repeat(" ", max(1, m.width-textWidth(line)-textWidth(right))) + right
}

func (m *Model) drawnWord() string {
	if m.drawn == opening {
		return ""
	}

	return m.drawn.String()
}
