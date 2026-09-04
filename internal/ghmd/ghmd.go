// Package ghmd segments a GitHub comment body into the blocks a terminal has to
// draw differently.
//
// It is not a markdown implementation and does not want to become one. A review
// screen needs to know four things a paragraph wrapper cannot answer: where a
// fenced block starts and ends, so its lines are truncated rather than reflowed;
// which lines are a table, for the same reason; what a <details> section holds,
// so it can be folded to its summary; and what is an HTML comment, so it is
// never shown at all. Everything else is prose, and the caller wraps it.
//
// The shapes it knows are the ones review bots write: fenced tool output inside
// collapsed sections, with routing metadata in HTML comments. Anything needing a
// real parser to recognize does not belong here.
package ghmd

import "strings"

// Kind is what a block is, which decides how it is drawn.
type Kind uint8

const (
	// Prose is everything with no structure worth keeping: wrap it.
	Prose Kind = iota
	// Code is a fenced block. Its lines are code and reflowing them is a lie
	// about what the code says, so they are truncated instead.
	Code
	// Quote is a > block, which is prose somebody else wrote.
	Quote
	// Table is a run of | rows. Reflowing one destroys the only thing it has.
	Table
	// Rule is a thematic break, drawn as one.
	Rule
	// Details is a collapsed section: a summary, and the blocks it holds.
	Details
)

// Block is one run of a body under one kind. A Details carries Blocks and no
// Lines; everything else carries Lines and no Blocks.
type Block struct {
	Kind Kind
	// Lang is the fence's tag, and empty on a fence that carried none.
	Lang string
	// Summary is what a Details section is folded to, and empty where it
	// declared none.
	Summary string
	Lines   []string
	Blocks  []Block
}

// Parse segments a body. It never fails and never drops a line it did not mean
// to: anything it does not recognize comes back as prose, spelled as it was
// written.
func Parse(body string) []Block {
	return blocks(strings.Split(hidden(body), "\n"))
}

// hidden removes HTML comments, which is where a bot keeps the state it needs
// and a reader needs to never see. A comment that is opened and never closed
// swallows the rest of the body, which is what a browser does with it too.
func hidden(body string) string {
	const open, shut = "<!--", "-->"

	var b strings.Builder

	for {
		at := strings.Index(body, open)
		if at < 0 {
			b.WriteString(body)

			return b.String()
		}

		b.WriteString(body[:at])

		end := strings.Index(body[at:], shut)
		if end < 0 {
			return b.String()
		}

		body = body[at+end+len(shut):]
	}
}

// scan is the position in a body's lines, so the block readers can advance it
// past what they consumed.
type scan struct {
	lines []string
	at    int
}

func (s *scan) done() bool   { return s.at >= len(s.lines) }
func (s *scan) line() string { return s.lines[s.at] }

func blocks(lines []string) []Block {
	s := &scan{lines: lines}

	var out []Block

	for !s.done() {
		if b, ok := s.structured(); ok {
			out = append(out, b...)

			continue
		}

		out = append(out, s.prose())
	}

	return trimmed(out)
}

// structured reads the one block starting at the cursor where it starts one of
// the shapes prose cannot carry, and reports false where the line is prose.
func (s *scan) structured() ([]Block, bool) {
	line := strings.TrimSpace(s.line())

	switch {
	case fenceOf(line) != "":
		return []Block{s.fence()}, true
	case strings.HasPrefix(line, "<details"):
		return []Block{s.details()}, true
	case isRule(line):
		s.at++

		return []Block{{Kind: Rule}}, true
	case strings.HasPrefix(line, "|"):
		return []Block{{Kind: Table, Lines: s.run(func(l string) bool {
			return strings.HasPrefix(strings.TrimSpace(l), "|")
		})}}, true
	case strings.HasPrefix(line, ">"):
		return []Block{{Kind: Quote, Lines: quoted(s.run(func(l string) bool {
			return strings.HasPrefix(strings.TrimSpace(l), ">")
		}))}}, true
	}

	return nil, false
}

// prose reads up to the next line that starts something else. A blank line ends
// it too, so one paragraph is one block and the space between two of them is
// not carried into either.
func (s *scan) prose() Block {
	var out []string

	for !s.done() {
		if _, structured := s.peek(); structured {
			break
		}

		line := s.line()
		s.at++

		if strings.TrimSpace(line) == "" {
			break
		}

		out = append(out, emphasis(line))
	}

	return Block{Kind: Prose, Lines: out}
}

// peek asks whether the line at the cursor starts a structured block without
// consuming anything.
func (s *scan) peek() (string, bool) {
	line := strings.TrimSpace(s.line())
	started := fenceOf(line) != "" || strings.HasPrefix(line, "<details") || isRule(line) ||
		strings.HasPrefix(line, "|") || strings.HasPrefix(line, ">")

	return line, started
}

// run takes every line the test admits, starting at the cursor.
func (s *scan) run(admits func(string) bool) []string {
	var out []string

	for !s.done() && admits(s.line()) {
		out = append(out, s.line())
		s.at++
	}

	return out
}

// fence reads a fenced block. The closing fence has to be at least as long as
// the opening one, which is how a body embeds a fence inside a fence, and a
// fence nobody closed runs to the end the way a browser renders it.
func (s *scan) fence() Block {
	open := fenceOf(strings.TrimSpace(s.line()))
	lang := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s.line()), open))
	s.at++

	var out []string

	for !s.done() {
		line := strings.TrimSpace(s.line())
		if shut := fenceOf(line); shut != "" && line == shut && len(shut) >= len(open) {
			s.at++

			break
		}

		out = append(out, s.line())
		s.at++
	}

	return Block{Kind: Code, Lang: lang, Lines: out}
}

// details reads a collapsed section, including the ones inside it. The summary
// is whatever the <summary> tag held, and a section without one is folded to a
// count of what it holds rather than to nothing.
func (s *scan) details() Block {
	s.at++

	summary := ""

	var inner []string

	depth := 1

	for !s.done() {
		line := strings.TrimSpace(s.line())

		switch {
		case strings.HasPrefix(line, "<details"):
			depth++
		case strings.HasPrefix(line, "</details>"):
			depth--
			if depth == 0 {
				s.at++

				return Block{Kind: Details, Summary: summary, Blocks: blocks(inner)}
			}
		case summary == "" && strings.HasPrefix(line, "<summary>"):
			summary = emphasis(strings.TrimSuffix(strings.TrimPrefix(line, "<summary>"), "</summary>"))
			s.at++

			continue
		}

		inner = append(inner, s.line())
		s.at++
	}

	return Block{Kind: Details, Summary: summary, Blocks: blocks(inner)}
}

// fenceOf is the fence a line opens or closes, and empty where it is neither.
func fenceOf(line string) string {
	const least = 3

	for _, mark := range []byte{'`', '~'} {
		n := 0
		for n < len(line) && line[n] == mark {
			n++
		}

		if n >= least {
			return line[:n]
		}
	}

	return ""
}

// isRule reports a thematic break: three or more of one mark and nothing else.
func isRule(line string) bool {
	const least = 3

	if line == "" || !strings.ContainsAny(line[:1], "-*_") {
		return false
	}

	mark := line[0]
	for i := range len(line) {
		if line[i] != mark {
			return false
		}
	}

	return len(line) >= least
}

// quoted takes the > and one space off every line of a quote, so the block is
// drawn as a quote rather than spelled as one.
func quoted(lines []string) []string {
	out := make([]string, len(lines))

	for i, line := range lines {
		out[i] = emphasis(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(line), ">"), " "))
	}

	return out
}

// trimmed drops the empty prose a blank line leaves behind.
func trimmed(in []Block) []Block {
	out := in[:0]

	for _, b := range in {
		if b.Kind == Prose && len(b.Lines) == 0 {
			continue
		}

		out = append(out, b)
	}

	return out
}
