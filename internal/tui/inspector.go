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

// panelRows is the inspector and the footer, which share one band at the bottom
// of the frame, so the body is the same height whatever the cursor is on.
func (l *List) panelRows() int { return l.inspectorRows() + 1 }

// panel is the bottom band: the inspector, then the footer, each to the full
// width on the panel's own background.
func (l *List) panel() string {
	lines := l.inspector()
	lines = append(lines, litLine(l.footer(), l.width, l.styles.panel))

	return strings.Join(lines, "\n")
}

// inspector draws the row under the cursor, one line per inspectorRows.
func (l *List) inspector() []string {
	row := l.current()
	if row == nil {
		blank := litLine("", l.width, l.styles.panel)

		return slices.Repeat([]string{blank}, l.inspectorRows())
	}

	about := aboutRow(row)
	facts := l.factLine(&about)

	if l.inspectorRows() == inspectorShort {
		return []string{l.panelLine(facts)}
	}

	return []string{l.panelLine(l.titleLine(&about)), l.panelLine(facts)}
}

func (l *List) titleLine(a *About) []Span {
	rest := []Span{}

	for _, text := range []string{a.Prefix, a.Ref} {
		if text != "" {
			rest = append(rest, Span{Text: titleGap}, Span{Text: text, Role: RoleQuiet})
		}
	}

	room := l.width - 1 - spansWidth(rest)
	subject := Span{Text: cut(a.Subject, max(subjectFloor, room)), Role: RoleText}

	return append([]Span{subject}, rest...)
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

	b.WriteString(face.Render(" "))

	room := l.width - 1

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
	if l.columns == nil {
		return nil
	}

	return l.gridLayout(bodyWidth(l.width, scrollbar(l.visible(), len(l.lines), l.offset))).Dropped
}

func (l *List) gridLayout(width int) table.Layout {
	return table.FitCompact(l.gridColumns(), width-gridMargin)
}

// aboutRow is what a row says of itself, or for a row that carries no About,
// what its fields amount to: the title, then the ref and meta.
func aboutRow(r *Row) About {
	if r.About != nil {
		return *r.About
	}

	subject := r.Under
	if subject == "" {
		subject = r.Tail
	}

	if subject == "" {
		subject = r.Left
	}

	a := About{Subject: subject}
	if subject != r.Left {
		a.Ref = r.Left
	}

	a.Facts = []Fact{{Text: r.Mid, Role: toneRole(r.Tone)}, {Text: r.Age}}

	if r.Under != "" {
		a.Facts = append(a.Facts, Fact{Text: r.Tail})
	}

	return About{
		Subject: a.Subject, Ref: a.Ref,
		Facts: append(a.Facts,
			Fact{Text: r.Remote, Role: toneRole(r.RemoteTone)},
			Fact{Text: r.Agent, Role: toneRole(r.AgentTone)}),
	}
}

func toneRole(t Tone) Role {
	switch t {
	case ToneGood:
		return RoleGood
	case ToneWarn:
		return RoleWarn
	case ToneBad:
		return RoleBad
	case ToneMuted:
		return RoleMuted
	case ToneMerged:
		return RoleMerged
	case ToneOrdinary:
	}

	return RoleCell
}
