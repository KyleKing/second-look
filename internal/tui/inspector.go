package tui

import (
	"slices"
	"strings"

	"github.com/kyleking/aragonite/tui/table"
)

// About is what the inspector says of a row: its title and where it lives on
// the first line, then the facts a grid column cannot hold on the second.
type About struct {
	// Subject is the title, with its conventional-commit Prefix and the full
	// Ref after it.
	Subject string
	Prefix  string
	Ref     string
	Facts   []Fact
}

// Fact is one entry of the inspector's second line.
type Fact struct {
	Text string
	Role Role
	// Column names a grid column. The fact is said only where that column was
	// hidden for want of room, which is how a narrow frame loses nothing it
	// cannot recover with one keypress.
	Column string
}

// The height from which the inspector has its title line, the line's two
// lengths, and the spacing its words keep.
const (
	inspectorTall  = 16
	inspectorFull  = 2
	inspectorShort = 1
	factGap        = " · "
	subjectFloor   = 8
	titleGap       = "  "
)

// inspectorRows is how many lines the inspector takes: the facts always, and
// the title above them where the frame is tall enough to spare it.
func (l *List) inspectorRows() int {
	if l.height < inspectorTall {
		return inspectorShort
	}

	return inspectorFull
}

// inspector draws the row under the cursor, one line per inspectorRows.
func (l *List) inspector() []string {
	row := l.current()
	if row == nil {
		return make([]string, l.inspectorRows())
	}

	about := aboutRow(row)
	facts := l.factLine(&about)

	if l.inspectorRows() == inspectorShort {
		return []string{l.panelLine(facts)}
	}

	return []string{l.panelLine(l.titleLine(&about)), l.panelLine(facts)}
}

// titleLine keeps the subject whole for as long as it can: the ref goes first
// and the prefix second, since the row already shows both.
func (l *List) titleLine(a *About) []Span {
	room := l.width

	for _, rest := range titleRests(a) {
		if textWidth(a.Subject) <= room-spansWidth(rest) {
			return append([]Span{{Text: a.Subject, Role: RoleText}}, rest...)
		}
	}

	return []Span{{Text: cut(a.Subject, max(subjectFloor, room)), Role: RoleText}}
}

// titleRests lists what the title line may keep beside the subject, most first.
func titleRests(a *About) [][]Span {
	var prefix, ref []Span

	if a.Prefix != "" {
		prefix = []Span{{Text: titleGap}, {Text: a.Prefix, Role: RoleQuiet}}
	}

	if a.Ref != "" {
		ref = []Span{{Text: titleGap}, {Text: a.Ref, Role: RoleQuiet}}
	}

	both := append(append([]Span{}, prefix...), ref...)

	return [][]Span{both, prefix, nil}
}

func (l *List) factLine(a *About) []Span {
	dropped := l.dropped()

	var out []Span

	for _, f := range a.Facts {
		if f.Text == "" || (f.Column != "" && !slices.Contains(dropped, f.Column)) {
			continue
		}

		if len(out) > 0 {
			out = append(out, Span{Text: factGap, Role: RolePending})
		}

		out = append(out, Span{Text: f.Text, Role: f.Role})
	}

	return out
}

// panelLine draws spans after the margin, cut to the width, on the panel's
// background. The title is lit and the rest is dim, since the title is the one
// thing on the line the cursor is asking about.
func (l *List) panelLine(spans []Span) string {
	face := l.styles.panel

	var b strings.Builder

	room := l.width

	for _, s := range spans {
		if room <= 0 {
			break
		}

		text := cut(s.Text, room)
		room -= textWidth(text)

		b.WriteString(face.Inherit(l.styles.role(s.Role, false, s.Role == RoleText)).Render(text))
	}

	b.WriteString(face.Render(strings.Repeat(" ", max(0, room))))

	return b.String()
}

func spansWidth(spans []Span) int {
	n := 0
	for _, s := range spans {
		n += textWidth(s.Text)
	}

	return n
}

// dropped is the grid columns the current width hid.
func (l *List) dropped() []string {
	return l.gridLayout(bodyWidth(l.width, scrollbar(l.visible(), len(l.lines), l.offset))).Dropped
}

func (l *List) gridLayout(width int) table.Layout {
	return table.FitCompact(l.gridColumns(), width-gridMargin)
}

// aboutRow is what a row says of itself, which is nothing where it carries no
// About.
func aboutRow(r *Row) About {
	if r.About == nil {
		return About{}
	}

	return *r.About
}
