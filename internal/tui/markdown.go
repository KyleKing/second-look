package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The agent answers in markdown and the pane is not a browser, so the few
// marks that carry meaning — emphasis, code, headings, rules, quotes,
// fences — are drawn and the syntax itself is dropped. It is a renderer for
// transcript prose, not a markdown engine: a mark that does not close stays
// literal rather than being guessed at.
type mdSpan struct {
	text string
	face lipgloss.Style
}

// markdownLines renders one entry's text into styled lines no wider than
// width, with base the face unmarked text draws in.
func (m *Model) markdownLines(text string, base lipgloss.Style, width int) []string {
	var out []string

	fenced := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case fenced:
			if isFence(trimmed) {
				fenced = false
			} else {
				out = append(out, m.styles.code.Render(cut(line, width)))
			}
		case isFence(trimmed):
			fenced = true
		case isHRule(trimmed):
			out = append(out, m.styles.hunk.Render(strings.Repeat("─", width)))
		case isHeading(trimmed):
			rest := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			out = append(out, wrapSpans(mdInline(rest, m.styles.head, m.styles.code), rest, width)...)
		case strings.HasPrefix(trimmed, "> "):
			out = append(out, wrapSpans(mdInline(trimmed[2:], base.Italic(true), m.styles.code), trimmed[2:], width)...)
		default:
			out = append(out, wrapSpans(mdInline(line, base, m.styles.code), line, width)...)
		}
	}

	return out
}

// runMin is how many of a mark it takes before it reads as a mark: fences and
// rules want three of one character, pairs want the one pair.
const runMin = 3

// isFence marks a code fence's edge: a run of backticks or tildes and nothing
// else.
func isFence(trimmed string) bool {
	return isRunOf(trimmed, "`~")
}

// isHRule marks a markdown rule: a run of one rule character and nothing
// else. A line of asterisks reads as a rule, not unclosed emphasis.
func isHRule(trimmed string) bool {
	return isRunOf(trimmed, "-*_")
}

func isRunOf(trimmed, chars string) bool {
	if len(trimmed) < runMin {
		return false
	}

	for _, r := range trimmed {
		if r != rune(trimmed[0]) || !strings.ContainsRune(chars, r) {
			return false
		}
	}

	return true
}

func isHeading(trimmed string) bool {
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}

	return i >= 1 && i <= 6 && i < len(trimmed) && trimmed[i] == ' '
}

// mdInline parses one line's inline marks into spans: code runs, bold,
// italic, strike, and links. Marks are tried longest-first at each byte, and
// one that never closes is left literal rather than swallowed.
func mdInline(line string, base, code lipgloss.Style) []mdSpan {
	var out []mdSpan

	for at := 0; at < len(line); {
		inner, face, consumed, ok := mdMarker(line, at, base, code)
		if !ok {
			at++

			continue
		}

		if at > 0 {
			out = append(out, mdSpan{line[:at], base})
		}

		out = append(out, mdSpan{inner, face})
		line = line[at+consumed:]
		at = 0
	}

	if line != "" {
		out = append(out, mdSpan{line, base})
	}

	return out
}

// mdMarker is the inline mark opening at at, with its inner text, the face it
// draws in, and how much of line it spans, or ok false where none opens.
func mdMarker(line string, at int, base, code lipgloss.Style) (string, lipgloss.Style, int, bool) {
	switch {
	case line[at] == '`':
		return mdCodeSpan(line, at, code)
	case strings.HasPrefix(line[at:], "**"):
		return mdPaired(line, at, "**", base.Bold(true))
	case strings.HasPrefix(line[at:], "__"):
		return mdPaired(line, at, "__", base.Bold(true))
	case strings.HasPrefix(line[at:], "~~"):
		return mdPaired(line, at, "~~", base.Strikethrough(true))
	case line[at] == '*' && at+1 < len(line) && line[at+1] != ' ':
		return mdPaired(line, at, "*", base.Italic(true))
	case line[at] == '_' && mdWordEdge(line, at):
		return mdPaired(line, at, "_", base.Italic(true))
	case line[at] == '[':
		return mdLink(line, at, base)
	}

	return "", base, 0, false
}

// mdCodeSpan is a run of n backticks closed by the same run; its inner text
// draws literal, marks and all.
func mdCodeSpan(line string, at int, code lipgloss.Style) (string, lipgloss.Style, int, bool) {
	n := 1
	for at+n < len(line) && line[at+n] == '`' {
		n++
	}

	fence := strings.Repeat("`", n)
	if i := strings.Index(line[at+n:], fence); i > 0 {
		return line[at+n : at+n+i], code, n + i + n, true
	}

	return "", code, 0, false
}

// mdPaired is a mark that closes with itself — `**`, `*`, `_`, `~~` — at at,
// returning an empty inner span nowhere so `** text` never eats the line.
func mdPaired(line string, at int, mark string, face lipgloss.Style) (string, lipgloss.Style, int, bool) {
	rest := line[at+len(mark):]
	if i := strings.Index(rest, mark); i > 0 {
		return rest[:i], face, len(mark) + i + len(mark), true
	}

	return "", face, 0, false
}

// mdLink is `[text](target)`: the text keeps, underlined; the target drops.
func mdLink(line string, at int, base lipgloss.Style) (string, lipgloss.Style, int, bool) {
	mid := strings.Index(line[at:], "](")
	if mid <= 0 {
		return "", base, 0, false
	}

	end := strings.IndexByte(line[at+mid+len("]("):], ')')
	if end <= 0 {
		return "", base, 0, false
	}

	return line[at+1 : at+mid], base.Underline(true), mid + len("](") + end + len(")"), true
}

// mdWordEdge is whether the _ at i can open emphasis: emphasis markers do not
// open inside a word, which is what keeps snake_case literal.
func mdWordEdge(line string, i int) bool {
	if i > 0 {
		prev := line[i-1]
		if prev != ' ' && prev != '\t' && prev != '(' && prev != '[' && prev != '"' && prev != '\'' {
			return false
		}
	}

	return i+1 < len(line) && line[i+1] != ' ' && line[i+1] != '_'
}

// mdWord is one wrapped word and the face it draws in; glue marks a word that
// touches the one before it, so a mark's edge does not grow a space.
type mdWord struct {
	text string
	face lipgloss.Style
	glue bool
}

// wrapSpans wraps styled spans to width under the same hang rules wrap uses:
// the plain text's own indent leads the first line and a list marker's width
// hangs the rest, so a wrapped bullet still reads as one item.
func wrapSpans(spans []mdSpan, plain string, width int) []string {
	if width < 1 {
		width = 1
	}

	body := strings.TrimLeft(plain, " \t")
	lead := strings.Repeat(" ", textWidth(strings.ReplaceAll(plain[:len(plain)-len(body)], "\t", "    ")))
	hang := lead + strings.Repeat(" ", marker(body))
	avail := max(1, width-textWidth(hang))

	var (
		out  []string
		cur  []mdWord
		cols int
	)

	emit := func() {
		out = append(out, emitLine(cur, lead, hang, len(out) == 0))
		cur, cols = nil, 0
	}

	words := mdWords(spans)
	for i := range words {
		w := &words[i]
		wide := textWidth(w.text)
		sep := 1
		if w.glue {
			sep = 0
		}

		switch {
		case len(cur) == 0:
			cur = append(cur, *w)
			cols = wide
		case cols+sep+wide <= avail:
			cur = append(cur, *w)
			cols += sep + wide
		default:
			emit()
			cur = append(cur, *w)
			cols = wide
		}

		// A word alone on its line and still too wide breaks into pieces,
		// the way a URL in a script with no spaces to break on has to.
		for cols > avail {
			pieces := split(w.text, avail)
			for _, p := range pieces[:len(pieces)-1] {
				cur = []mdWord{{text: p, face: w.face}}
				emit()
			}

			last := pieces[len(pieces)-1]
			cur = []mdWord{{text: last, face: w.face}}
			cols = textWidth(last)
		}
	}

	emit()

	return out
}

// emitLine is the words of one wrapped line drawn, lead where it is the
// first, hang where it is a wrap's continuation.
func emitLine(cur []mdWord, lead, hang string, first bool) string {
	var b strings.Builder
	if first {
		b.WriteString(lead)
	} else {
		b.WriteString(hang)
	}

	for i := range cur {
		if i > 0 && !cur[i].glue {
			b.WriteByte(' ')
		}

		b.WriteString(cur[i].face.Render(cur[i].text))
	}

	return b.String()
}

// mdWords is the spans flattened to words. Spans slice the line without gaps,
// so a word that opens a span touches the last word of the span before it
// unless either side had a space: "`code`," stays "code," rather than
// drifting to "code ,".
func mdWords(spans []mdSpan) []mdWord {
	var words []mdWord

	spaced := true
	for i := range spans {
		text := spans[i].text
		headSpace := text == "" || text[0] == ' ' || text[0] == '\t'
		for j, w := range strings.Fields(text) {
			words = append(words, mdWord{text: w, face: spans[i].face, glue: j == 0 && !spaced && !headSpace})
		}
		spaced = text == "" || text[len(text)-1] == ' ' || text[len(text)-1] == '\t'
	}

	return words
}
