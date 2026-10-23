package tui

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/highlight"
)

// nameLetters label the names on a line, home row first. The j and k that walk
// a peek and the q that leaves are never a label.
const nameLetters = "asdfghlwertyuiopzxcvbnm"

type ident struct {
	name string
	at   int
}

// pick is a gd or gr waiting for the letter of the name it asks about.
type pick struct {
	kind  diag.JumpKind
	row   int
	path  string
	line  int
	names []ident
}

// identsOf is the names on a code row: every Go-style identifier, less what the
// grammar reads as a comment, a string, a number, or a keyword. A line the
// grammar says nothing about keeps every identifier.
func (m *Model) identsOf(r row) []ident {
	text := r.line.Text
	spans := m.spansFor(r, r.line)

	var out []ident

	for i := 0; i < len(text); {
		rn, size := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(rn) && !unicode.IsDigit(rn) && rn != '_' {
			i += size

			continue
		}

		end := i + size
		for end < len(text) {
			next, n := utf8.DecodeRuneInString(text[end:])
			if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' {
				break
			}

			end += n
		}

		if !unicode.IsDigit(rn) && !notCode(spans, i) {
			out = append(out, ident{name: text[i:end], at: i})
		}

		i = end
	}

	return out
}

func notCode(spans []highlight.Span, at int) bool {
	switch classAt(spans, at) {
	case highlight.Comment, highlight.String, highlight.Number, highlight.Keyword:
		return true
	case highlight.Plain, highlight.Name, highlight.Function, highlight.Type, highlight.Punctuation:
	}

	return false
}

// lookup answers the g chord: where a name on the line under the cursor is
// declared, or everywhere it is read. A line with several names labels each
// and waits for a letter, and a line with one asks about it at once.
func (m *Model) lookup(kind diag.JumpKind) tea.Cmd {
	if m.prober == nil {
		m.noProber()

		return nil
	}

	verb := verbOf(kind)

	r := m.screen.rows[m.cursor]
	if r.kind != rowCode || r.path == "" || r.line.New == 0 {
		m.say("g's "+verb+" asks about a line of the change", false)

		return nil
	}

	names := m.identsOf(r)

	switch len(names) {
	case 0:
		m.say("no name on line "+strconv.Itoa(r.line.New), false)

		return nil
	case 1:
		return m.askName(kind, r, names[0])
	}

	names = names[:min(len(names), len(nameLetters))]
	m.picking = &pick{kind: kind, row: m.cursor, path: r.path, line: r.line.New, names: names}
	m.say(m.pickPrompt(), false)

	return nil
}

func verbOf(kind diag.JumpKind) string {
	if kind == diag.References {
		return "used"
	}

	return "defined"
}

func (m *Model) pickPrompt() string {
	var b strings.Builder

	b.WriteString("which name is " + verbOf(m.picking.kind) + "? ")

	for i, n := range m.picking.names {
		b.WriteString(string(nameLetters[i]) + " " + n.name + "  ")
	}

	return b.String() + "esc cancels"
}

// pickName takes the letter that chooses a name, or the key that gives up.
func (m *Model) pickName(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.picking

	if key.Matches(msg, m.keys.Quit) {
		m.picking = nil
		m.say("", false)

		return m, nil
	}

	at := strings.Index(nameLetters[:len(p.names)], msg.String())
	if len(msg.String()) != 1 || at < 0 {
		m.say("no name is labeled "+msg.String()+"; esc cancels", false)

		return m, nil
	}

	m.picking = nil
	cmd := m.askName(p.kind, m.screen.rows[p.row], p.names[at])

	return m, cmd
}

// askName puts one name to the language server.
func (m *Model) askName(kind diag.JumpKind, r row, name ident) tea.Cmd {
	m.say("asking where "+name.name+" is "+verbOf(kind)+"…", false)

	ask, path, line := m.prober, r.path, r.line.New

	return func() tea.Msg {
		jump, err := ask.Jumps(context.Background(), path, line, name.at, kind)

		return peekedMsg{kind: kind, path: path, line: line, jump: jump, err: err}
	}
}

// labeled draws each pick's letter over the first cell of its name, in the
// row's own frame lines. Which lines and columns a name falls on follows the
// layout the row was drawn in, so a name past a cut or in a half the row does
// not draw is left unlabeled.
func (m *Model) labeled(lines []string, r row, width int) []string {
	origin, room := m.codeBox(width)
	if room < 1 {
		return lines
	}

	for i, n := range m.picking.names {
		cells := textWidth(expand(r.line.Text[:n.at]))

		k, x := 0, cells
		if m.drawn.wrap {
			k, x = cells/room, cells%room
		}

		if k >= len(lines) || x >= room {
			continue
		}

		first, _ := utf8.DecodeRuneInString(n.name)
		span := max(1, textWidth(string(first)))
		chip := m.styles.cursor.Render(string(nameLetters[i]) + strings.Repeat(" ", span-1))
		col := origin + x

		lines[k] = ansi.Cut(lines[k], 0, col) + chip + ansi.Cut(lines[k], col+span, textWidth(lines[k]))
	}

	return lines
}

// codeBox is where a code row's text starts in its frame line and how many
// cells it has, which side by side is the right half.
func (m *Model) codeBox(width int) (int, int) {
	if !m.sideBySide() {
		return m.gutterWidth(), width - m.gutterWidth()
	}

	const divider = 3

	half := (width - divider) / sides
	origin := half + divider + m.gutterWidth()

	return origin, width - half - divider - m.gutterWidth()
}
