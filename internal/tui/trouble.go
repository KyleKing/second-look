package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
)

// Prober is what a review can ask about its own files: what a checker makes of
// them, and what the names on one line actually are.
//
// It is a parameter so the screen does not depend on how either is answered. A
// review with none runs as it always has, which is what a checkout nobody has
// a language server for gets.
type Prober interface {
	// Notes is every checker's answer for the files under review. It is slow
	// the first time, because a language server loads the project before it
	// says anything, and fast afterwards.
	Notes(ctx context.Context) ([]diag.Note, error)
	// Hover is what each name on one line resolves to.
	Hover(ctx context.Context, path string, line int) ([]diag.Symbol, error)
	// Close ends whatever is still running.
	Close()
}

// WithProber gives the review a checker to ask. Without it the trouble list
// says nothing is configured, which is the honest answer: a screen that hid the
// key would leave a reader assuming the change is clean.
func WithProber(p Prober) Option {
	return func(m *Model) { m.prober = p }
}

// close ends whatever the review had running. It is idempotent, because the
// shell closes a review it drops and again when it ends.
func (m *Model) close() {
	if m.prober != nil {
		m.prober.Close()
		m.prober = nil
	}
}

// notesMsg carries one pass over the review's files.
type notesMsg struct {
	notes []diag.Note
	err   error
}

// probe asks every checker about the review's files, behind the first frame.
//
// It runs as a command for the reason the structural pass does, and more so: a
// language server loading a monorepo costs seconds, and a screen that would not
// answer a key until it finished is a screen that looks broken.
func (m *Model) probe() tea.Cmd {
	if m.prober == nil {
		return nil
	}

	m.probing = true
	ask := m.prober

	return func() tea.Msg {
		notes, err := ask.Notes(context.Background())

		return notesMsg{notes: notes, err: err}
	}
}

// applyNotes takes what the checkers found.
func (m *Model) applyNotes(msg notesMsg) {
	m.probing = false
	m.trouble = diag.Place(m.diff, msg.notes)
	m.troubled = msg.err

	// A pass that failed outright says so, because a trouble list that is empty
	// because nothing ran reads exactly like one that is empty because the
	// change is clean.
	if msg.err != nil && len(msg.notes) == 0 {
		m.say("checking: "+msg.err.Error(), true)
	}

	m.rebuild()
}

// troubleWord is what the title says about the pass, which is where a count
// belongs: a reader deciding whether to press X wants the number, not the list.
func (m *Model) troubleWord() string {
	if m.prober == nil {
		return ""
	}

	if m.probing {
		return "checking…"
	}

	if n := m.trouble.Count(); n > 0 {
		return plural(n, "problem")
	}

	return ""
}

// hover answers K: every name on the line under the cursor, and what the
// language server says each one is.
//
// A review screen has no column cursor, so the line is asked about at each of
// its names rather than at a point. That is the question a diff cannot settle
// and the reason this exists: whether the field being read off a value is a
// member of the type it actually has.
func (m *Model) hover() tea.Cmd {
	if m.prober == nil {
		m.say("no language server is configured for this review", false)

		return nil
	}

	r := m.screen.rows[m.cursor]
	if r.kind != rowCode || r.path == "" || r.line.New == 0 {
		m.say("K asks about a line of the change", false)

		return nil
	}

	m.say("asking about line "+strconv.Itoa(r.line.New)+"…", false)

	ask, path, line := m.prober, r.path, r.line.New

	return func() tea.Msg {
		syms, err := ask.Hover(context.Background(), path, line)

		return hoverMsg{path: path, line: line, symbols: syms, err: err}
	}
}

type hoverMsg struct {
	path    string
	line    int
	symbols []diag.Symbol
	err     error
}

func (m *Model) applyHover(msg hoverMsg) {
	switch {
	case msg.err != nil:
		m.say("asking about line "+strconv.Itoa(msg.line)+": "+msg.err.Error(), true)
	case len(msg.symbols) == 0:
		m.say("nothing on line "+strconv.Itoa(msg.line)+" has a type to show", false)
	default:
		m.showing = &msg
		m.say("", false)
	}
}

// troubleRows is what is said about one line, drawn under it.
//
// A note goes under its line rather than into the gutter because the message is
// the whole of it: a mark in the margin says a line is wrong and leaves the
// reader to go and find out how, which is the trip out to an editor this exists
// to save.
func troubleRows(notes []diag.Note, path string, numWidth, width int) []row {
	var out []row

	room := max(minTroubleWidth, width-numWidth-indent-troubleRail)

	for _, n := range notes {
		for i, line := range wrap(troubleWord(n), room) {
			out = append(out, row{
				kind: rowTrouble, text: line, path: path, comment: noComment,
				severity: n.Severity, head: i == 0,
			})
		}
	}

	return out
}

// The trouble rail is the marker and the space after it, and the floor is what
// is left to wrap into on a frame too narrow to have anything.
const (
	troubleRail     = 2
	minTroubleWidth = 20
)

// troubleWord is one note as a line: who said it, under what rule, and what
// they said. The rule is there because a note nobody can trace to a rule is a
// note nobody can turn off.
func troubleWord(n diag.Note) string {
	head := n.Source
	if n.Code != "" {
		head += " " + n.Code
	}

	if head == "" {
		return n.Message
	}

	return head + ": " + n.Message
}

// buildTrouble is every note on the review, each under the line it lands on,
// and nothing else.
//
// It is the diff view's own answer read the other way round. A note under its
// line answers "what is wrong with this", and a reader who has not read the
// diff yet has the other question: what is wrong anywhere, and which file
// should be opened because of it.
func buildTrouble(d *diff.Diff, t diag.Placed, probing bool, lay layout) screen {
	s := screen{numWidth: numberWidth(d)}
	at := anchorLines(d)

	for _, path := range troublePaths(t) {
		if len(s.rows) > 0 {
			s.rows = append(s.rows, row{kind: rowBlank, comment: noComment})
		}

		s.rows = append(s.rows, row{
			kind: rowFile, path: path, comment: noComment,
			text: fmt.Sprintf("%s  %s", path, plural(countTrouble(t, path), "problem")),
		})

		s.rows = append(s.rows, onLines(t, path, at, s.numWidth, lay.width)...)

		// A note on a line nobody touched is context rather than a finding, so
		// it is listed under the changed ones with its own line number and
		// without the code around it.
		for _, n := range t.Elsewhere[path] {
			s.rows = append(s.rows, elsewhereRows(n, path, s.numWidth, lay.width)...)
		}
	}

	if len(s.rows) == 0 {
		s.rows = append(s.rows, row{
			kind: rowFile, comment: noComment, text: emptyTroubleWord(probing),
		})
	}

	return s
}

func emptyTroubleWord(probing bool) string {
	if probing {
		return "still checking…"
	}

	return "nothing to report on the lines this change wrote"
}

// onLines is every note anchored to a changed line of one file, each under the
// line itself so the prose is read with the code it is about.
func onLines(t diag.Placed, path string, at map[anchor]diff.Line, numWidth, width int) []row {
	var out []row

	for _, line := range troubleLines(t, path) {
		ln, ok := at[anchor{path: path, side: artifact.SideRight, line: line}]
		if ok {
			out = append(out, row{
				kind: rowCode, line: ln, path: path, comment: noComment, hunk: ln.Hunk,
			})
		}

		out = append(out, troubleRows(t.On[diag.Anchor{Path: path, Line: line}],
			path, numWidth, width)...)
	}

	return out
}

// elsewhereRows is one note on an unchanged line, which carries its own line
// number because there is no code drawn above it to say where it is.
func elsewhereRows(n diag.Note, path string, numWidth, width int) []row {
	rows := troubleRows([]diag.Note{n}, path, numWidth, width)
	if len(rows) > 0 {
		rows[0].text = fmt.Sprintf("line %d · %s", n.Line, rows[0].text)
	}

	return rows
}

// troublePaths is every file carrying a note, in the order a reader looks for
// them rather than the order a checker answered in.
func troublePaths(t diag.Placed) []string {
	seen := map[string]bool{}

	for at := range t.On {
		seen[at.Path] = true
	}

	for path := range t.Elsewhere {
		seen[path] = true
	}

	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}

	sort.Strings(out)

	return out
}

// troubleLines is the changed lines of one file that carry a note, in file
// order.
func troubleLines(t diag.Placed, path string) []int {
	var out []int

	for at := range t.On {
		if at.Path == path {
			out = append(out, at.Line)
		}
	}

	sort.Ints(out)

	return out
}

func countTrouble(t diag.Placed, path string) int {
	n := len(t.Elsewhere[path])

	for at, notes := range t.On {
		if at.Path == path {
			n += len(notes)
		}
	}

	return n
}

// troubleRow draws one note under its line. The marker is a glyph rather than
// a color alone, so a monochrome terminal keeps the distinction between what is
// wrong and what is merely worth knowing.
func (m *Model) troubleRow(r row) (string, lipgloss.Style) {
	mark := "  "
	if r.head {
		mark = troubleMark(r.severity) + " "
	}

	return strings.Repeat(" ", m.screen.numWidth+indent) + mark + r.text,
		m.troubleStyle(r.severity)
}

func troubleMark(s diag.Severity) string {
	switch s {
	case diag.Error:
		return "\u00d7"
	case diag.Warning:
		return "!"
	case diag.Info, diag.Hint:
		return "\u00b7"
	}

	return "\u00b7"
}

func (m *Model) troubleStyle(s diag.Severity) lipgloss.Style {
	switch s {
	case diag.Error:
		return m.styles.fail
	case diag.Warning:
		return m.styles.warn
	case diag.Info, diag.Hint:
		return m.styles.note
	}

	return m.styles.note
}

// hoverLines is the answer K left up: each name on the line and what it is.
func (m *Model) hoverLines() []string {
	out := []string{
		m.styles.head.Render(fmt.Sprintf("%s line %d", m.showing.path, m.showing.line)),
		"",
	}

	room := max(minTroubleWidth, m.width-indent*2)

	for _, sym := range m.showing.symbols {
		for i, line := range wrap(sym.Text, room) {
			if i == 0 {
				out = append(out, m.styles.body.Render("  "+line))

				continue
			}

			out = append(out, m.styles.note.Render("    "+line))
		}
	}

	return append(out, "", m.styles.footer.Render("  any key to go back"))
}
