package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/highlight"
)

// kept is how many trailing parts of a path outside the checkout a site names.
const kept = 2

// peekLines is how much of a definition's file its card shows, and peekAbove how
// many of those lines come before the declaration, so a doc comment is in view.
const (
	peekLines = 12
	peekAbove = 2
	peekRefs  = 20
)

// peekCard is the answer to a gd or gr, drawn as a card under the line that
// asked. It lives on the screen only: nothing about it is saved or posted.
type peekCard struct {
	kind  diag.JumpKind
	name  string
	path  string
	line  int
	sites []diag.Site
	// placing is true from the card's opening until the first key, while the
	// file it draws may still arrive and move the lines under the cursor.
	placing bool
}

// peekedMsg is a language server's answer about one name.
type peekedMsg struct {
	kind diag.JumpKind
	path string
	line int
	jump diag.Jump
	err  error
}

func peekKeys() [][2]string {
	return [][2]string{{enterKey, "open"}, {walkKeys, ""}, {escKey, "close"}}
}

func (m *Model) applyPeeked(msg peekedMsg) tea.Cmd {
	where := strconv.Itoa(msg.line)

	switch {
	case msg.err != nil:
		m.say("asking about line "+where+": "+msg.err.Error(), true)
	case len(msg.jump.At) == 0:
		m.say(msg.jump.Name+" is not "+verbOf(msg.kind)+" anywhere", false)
	case !m.onAnchor(msg.path, msg.line):
		m.say("the cursor moved on before "+msg.jump.Name+" was answered", false)
	default:
		m.jumps.push(m.snapshot())

		m.peeked = &peekCard{
			kind: msg.kind, name: msg.jump.Name, path: msg.path, line: msg.line, sites: msg.jump.At,
			placing: true,
		}
		m.say("", false)
		m.rebuild()
		m.enterPeek()

		return m.peekReads(msg.jump.At)
	}

	return nil
}

func (m *Model) onAnchor(path string, line int) bool {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) {
		return false
	}

	r := m.screen.rows[m.cursor]

	return r.kind == rowCode && r.path == path && r.line.New == line
}

// enterPeek puts the cursor on the definition's own line, or on the first
// reference, and frames the card.
func (m *Model) enterPeek() {
	first := -1

	for i := range m.screen.rows {
		r := &m.screen.rows[i]
		if r.kind != rowPeek || r.role != roleBody {
			continue
		}

		if first < 0 {
			first = i
		}

		if m.peeked.kind == diag.Definitions && r.site == m.peeked.sites[0] {
			first = i

			break
		}
	}

	if first >= 0 {
		m.cursor = first
		m.reveal()
	}
}

// splicePeek puts the card's rows under the line it answers. A line the screen
// no longer draws takes the card with it.
func (m *Model) splicePeek() {
	p := m.peeked
	if p == nil {
		return
	}

	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.kind == rowCode && r.path == p.path && r.line.New == p.line {
			m.screen.rows = slices.Insert(m.screen.rows, i+1, m.peekRows()...)

			return
		}
	}

	m.peeked = nil
}

// peekSpan is the rows the card occupies, and false where there is none.
func (m *Model) peekSpan() (int, int, bool) {
	first, last := -1, -1

	for i := range m.screen.rows {
		if m.screen.rows[i].kind != rowPeek {
			continue
		}

		if first < 0 {
			first = i
		}

		last = i
	}

	return first, last, first >= 0
}

// closePeek takes the card down and keeps the cursor on what it was on: the
// line that asked when it was inside the card, and the same row when it was
// below it.
func (m *Model) closePeek() {
	first, last, ok := m.peekSpan()
	if !ok {
		m.peeked = nil

		return
	}

	cursor := m.cursor

	switch {
	case cursor > last:
		cursor -= last - first + 1
	case cursor >= first:
		cursor = first - 1
	}

	m.peeked = nil
	m.rebuild()
	m.cursor = clamp(cursor, len(m.screen.rows)-1)
	m.follow()
}

// settlePeek closes the card once the cursor has left it.
func (m *Model) settlePeek() {
	if m.peeked == nil {
		return
	}

	m.peeked.placing = false

	if first, last, ok := m.peekSpan(); ok && (m.cursor < first || m.cursor > last) {
		m.closePeek()
	}
}

// peekRows is the card: its title, one line per row of the definition or per
// reference, and the foot that says what the keys do.
func (m *Model) peekRows() []row {
	p := m.peeked
	head := row{kind: rowPeek, comment: noComment, head: true, site: p.sites[0]}

	var body []row

	switch p.kind {
	case diag.References:
		head.text = fmt.Sprintf("⇢ %s · %s", p.name, plural(len(p.sites), "reference"))
		body = m.referenceRows(p.sites)
	case diag.Definitions:
		head.text = fmt.Sprintf("⇢ %s · %s", p.name, siteWord(p.sites[0]))

		if more := len(p.sites) - 1; more > 0 {
			head.aside = fmt.Sprintf("+%d more", more)
		}

		body = m.definitionRows(p.sites[0])
	}

	return framed(append([]row{head}, body...), row{kind: rowPeek, comment: noComment}, m.diffWidth())
}

func (m *Model) definitionRows(site diag.Site) []row {
	lines, ok := m.blobs[site.Path]
	if !ok {
		return []row{{kind: rowPeek, comment: noComment, text: "reading " + siteWord(site) + "…", site: site}}
	}

	lo := max(1, site.Line-peekAbove)
	hi := min(len(lines), lo+peekLines-1)

	if lo > hi {
		text := siteWord(site) + " is past the end of the file"

		return []row{{kind: rowPeek, comment: noComment, text: text, site: site}}
	}

	spans := highlight.Lines(site.Path, lines[lo-1:hi])
	num := len(strconv.Itoa(hi))
	out := make([]row, 0, hi-lo+1)

	for at := lo; at <= hi; at++ {
		mark := "  "
		if at == site.Line {
			mark = "› "
		}

		prefix := fmt.Sprintf("%s%*d  ", mark, num, at)
		r := row{
			kind: rowPeek, comment: noComment, text: prefix + expand(lines[at-1]),
			site: diag.Site{Path: site.Path, Line: at},
		}

		if at-lo < len(spans) {
			r.lit = shifted(spans[at-lo], lines[at-1], len(prefix))
		}

		out = append(out, r)
	}

	return out
}

// shifted moves a line's spans onto a row whose text starts with a prefix and
// has its tabs expanded, since the spans index the line as the file spells it.
func shifted(spans []highlight.Span, line string, prefix int) []highlight.Span {
	out := make([]highlight.Span, 0, len(spans))

	for _, s := range spans {
		from := prefix + textWidth(expand(line[:s.From]))
		out = append(out, highlight.Span{From: from, To: from + textWidth(expand(line[s.From:s.To])), Class: s.Class})
	}

	return out
}

func (m *Model) referenceRows(sites []diag.Site) []row {
	out := make([]row, 0, min(len(sites), peekRefs)+1)

	for _, s := range sites[:min(len(sites), peekRefs)] {
		text := siteWord(s)

		if lines, ok := m.blobs[s.Path]; ok && s.Line >= 1 && s.Line <= len(lines) {
			text += "  " + strings.TrimSpace(expand(lines[s.Line-1]))
		}

		out = append(out, row{kind: rowPeek, comment: noComment, text: text, site: s})
	}

	if rest := len(sites) - peekRefs; rest > 0 {
		out = append(out, row{kind: rowPeek, comment: noComment, text: fmt.Sprintf("+%d more", rest)})
	}

	return out
}

// siteWord spells a place as file:line, keeping the last two parts of a path
// outside the checkout, where the whole of it is no help.
func siteWord(s diag.Site) string {
	name := s.Path

	if filepath.IsAbs(name) {
		parts := strings.Split(filepath.ToSlash(name), "/")
		name = "…/" + strings.Join(parts[max(0, len(parts)-kept):], "/")
	}

	return name + ":" + strconv.Itoa(s.Line)
}

// peekReads asks for the files the sites live in, where no hunk's growth
// already did, so the card can draw their lines once they arrive.
func (m *Model) peekReads(sites []diag.Site) tea.Cmd {
	if m.blob == nil {
		return nil
	}

	var cmds []tea.Cmd

	read := map[string]bool{}

	for _, s := range sites {
		if len(cmds) >= peekReadsMax {
			break
		}

		if _, held := m.blobs[s.Path]; held || read[s.Path] {
			continue
		}

		read[s.Path] = true

		cmds = append(cmds, m.peekBlob(s.Path))
	}

	return tea.Batch(cmds...)
}

// peekReadsMax is how many files one answer's lines may read.
const peekReadsMax = 8
