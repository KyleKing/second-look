package tui

import (
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/highlight"
)

// renderMode is how a line of the diff is drawn. `v` cycles them.
//
// They are experiments and they are meant to be: each ships with its caveat in
// the help and the README, all of them get lived with on real reviews, and the
// ones that turn out not to earn a keystroke are deleted rather than kept for
// symmetry.
type renderMode int

const (
	// A whole-line color per side and one number per line, which is what a
	// terminal diff has always looked like.
	renderPlain renderMode = iota
	// The grammar in color under a band saying which side the line is on, the
	// runs that actually changed marked inside it, and both line numbers in the
	// gutter.
	renderRich
)

func (r renderMode) next() renderMode {
	if r == renderRich {
		return renderPlain
	}

	return r + 1
}

func (r renderMode) String() string {
	if r == renderRich {
		return "rich"
	}

	return "plain"
}

// caveat is what the mode does not do, said where the mode is named. A spike
// whose limits are only in the commit message is one nobody can judge.
func (r renderMode) caveat() string {
	if r == renderRich {
		return "a hunk is a fragment, so a grammar's state above it is lost"
	}

	return ""
}

// depth is how far toward the middle a band's lightness is lifted and how much
// of the accent's saturation it keeps.
type depth struct {
	lift float64
	sat  float64
}

// The two depths a side is drawn at: the band under a whole changed line, and
// the runs inside it that actually differ from the line it is paired with. The
// text is colored by grammar, so the side has to be said by something that
// leaves the foreground alone.
//
// A terminal that can mix its own colors gets them held well back, because a
// band is read past rather than read and one at the accent's own saturation
// competes with the code sitting on it. A 256-color terminal gets them deeper,
// since the cube carries almost no dark tints: held back there, added and
// removed both quantize into the grey ramp and the band says nothing.
const (
	// What a terminal that mixes its own colors gets.
	trueBandLift, trueBandSat = 0.09, 0.45
	trueMarkLift, trueMarkSat = 0.20, 0.85
	// What a 256-color terminal gets, deep enough to survive the cube.
	cubeBandLift, cubeMarkLift = 0.14, 0.34
	cubeSat                    = 1.0
)

var (
	bandMix, markMix = depths(supportsMillions())

	trueBand = depth{lift: trueBandLift, sat: trueBandSat}
	trueMark = depth{lift: trueMarkLift, sat: trueMarkSat}
	cubeBand = depth{lift: cubeBandLift, sat: cubeSat}
	cubeMark = depth{lift: cubeMarkLift, sat: cubeSat}
)

func depths(millions bool) (depth, depth) {
	if millions {
		return trueBand, trueMark
	}

	return cubeBand, cubeMark
}

// supportsMillions asks what the terminal can actually mix, which decides how
// far back the bands are held.
func supportsMillions() bool {
	return colorprofile.Detect(os.Stdout, os.Environ()) == colorprofile.TrueColor
}

// richStyles are the faces the rich renderer draws with: nightfox's assignments
// derived into aragonite's palette, so second-look, gh-repo-dashboard, and
// gh-sweep stay one visual family rather than each carrying a theme.
type richStyles struct {
	class map[highlight.Class]lipgloss.Style
	// band and mark are the two depths of each side, indexed by the diff's own
	// spelling of the line kind. A context line is in neither, which is what
	// says it has no side.
	band map[byte]color.Color
	mark map[byte]color.Color
	// gutter is the two line numbers, dimmer than the code they index.
	gutter lipgloss.Style
}

func newRichStyles(s styles) richStyles {
	p, base := s.palette, lipgloss.NewStyle()

	return richStyles{
		class: map[highlight.Class]lipgloss.Style{
			highlight.Comment:     base.Foreground(p.Overlay1).Italic(true),
			highlight.Keyword:     base.Foreground(p.Mauve),
			highlight.String:      base.Foreground(p.Green),
			highlight.Number:      base.Foreground(p.Peach),
			highlight.Function:    base.Foreground(p.Blue),
			highlight.Type:        base.Foreground(p.Yellow),
			highlight.Name:        base.Foreground(p.Text),
			highlight.Punctuation: base.Foreground(p.Overlay2),
			highlight.Plain:       base.Foreground(p.Text),
		},
		band: map[byte]color.Color{
			diff.KindAdd:    blend(p.Base, p.Green, bandMix),
			diff.KindRemove: blend(p.Base, p.Red, bandMix),
		},
		mark: map[byte]color.Color{
			diff.KindAdd:    blend(p.Base, p.Green, markMix),
			diff.KindRemove: blend(p.Base, p.Red, markMix),
		},
		gutter: base.Foreground(p.Overlay0),
	}
}

// richCode draws one line of the diff at exactly width cells: both line
// numbers, the sign, and the text colored by grammar under the band for its
// side, with the runs that differ from the line it is paired with marked.
//
// The band runs to the frame's edge rather than stopping at the text, because
// a band that ended where a line ended would draw the length of every line as
// if it meant something.
func (m *Model) richCode(r row, width int) string {
	room := max(0, width-m.gutterWidth())
	body, cells := m.richText(r, room)

	if over := room - cells; over > 0 {
		body += m.padTo(r.line.Kind, over)
	}

	return m.richGutter(r.line) + cut(body, room)
}

// gutterWidth is what both numbers and the sign take, which every rich row
// spends whether or not it carries either number: the two numbers, a space
// between them, then the sign with a space either side.
func (m *Model) gutterWidth() int {
	const signAndSpaces = 4

	return m.screen.numWidth*2 + signAndSpaces
}

// richGutter is the old and the new number together. A line on one side leaves
// the other blank rather than repeating itself, so the column of numbers says
// at a glance which side each line is on even where color is gone.
func (m *Model) richGutter(l diff.Line) string {
	return m.rich.gutter.Render(fmt.Sprintf("%s %s %c ",
		number(l.Old, m.screen.numWidth), number(l.New, m.screen.numWidth), l.Kind))
}

func number(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}

	return fmt.Sprintf("%*d", width, n)
}

// padTo fills the rest of the frame with the line's own band, so a band reaches
// the edge rather than stopping where the text does and drawing the length of
// every line as if it meant something.
func (m *Model) padTo(kind byte, cells int) string {
	spaces := strings.Repeat(" ", cells)

	bg, ok := m.rich.band[kind]
	if !ok {
		return spaces
	}

	return lipgloss.NewStyle().Background(bg).Render(spaces)
}

// richText is the line's text, cut into runs by grammar and by what changed,
// each drawn with the color of its class over the background of its depth. It
// answers the cells it spent as well, since the band has to reach the frame's
// edge and the escapes make the string's own length useless for saying where
// that is.
//
// The runs index the line as the patch spells it, so tabs are expanded run by
// run against a running column rather than up front: expanding first would
// move every byte offset the grammar and the pairing answered in.
func (m *Model) richText(r row, width int) (string, int) {
	band, ok := m.rich.band[r.line.Kind]
	mark := m.rich.mark[r.line.Kind]
	marked := m.refined[refOf(r.path, r.line)]

	var (
		b    strings.Builder
		cols int
	)

	for _, piece := range m.runs(r, r.line.Text) {
		style := under(m.rich.class[piece.class], band, ok)
		if ok && covered(marked, piece.from) {
			style = under(m.rich.class[piece.class], mark, true).Bold(true)
		}

		text, spent := expandFrom(r.line.Text[piece.from:piece.to], cols)
		b.WriteString(style.Render(text))

		cols = spent
		if cols >= width {
			break
		}
	}

	return b.String(), cols
}

// expandFrom replaces the tabs of one run, given the cells already spent on the
// line, and answers where the line now stands. A tab advances to the next stop
// rather than writing a fixed number of cells, so a line drawn in pieces has to
// carry the column between them.
func expandFrom(s string, col int) (string, int) {
	var b strings.Builder

	for _, r := range s {
		if r != '\t' {
			b.WriteRune(r)
			col += textWidth(string(r))

			continue
		}

		n := tabStop - col%tabStop
		b.WriteString(strings.Repeat(" ", n))
		col += n
	}

	return b.String(), col
}

// run is one stretch of a line drawn with one face: a grammar class that does
// not straddle the boundary of a changed run.
type run struct {
	from  int
	to    int
	class highlight.Class
}

// runs cuts a line at every boundary either the grammar or the pairing draws,
// so a face never has to answer for two of them at once.
func (m *Model) runs(r row, text string) []run {
	cuts := map[int]bool{0: true, len(text): true}

	spans := m.spansFor(r)
	for _, s := range spans {
		cuts[min(s.From, len(text))], cuts[min(s.To, len(text))] = true, true
	}

	for _, s := range m.refined[refOf(r.path, r.line)] {
		cuts[min(s.From, len(text))], cuts[min(s.To, len(text))] = true, true
	}

	at := make([]int, 0, len(cuts))
	for c := range cuts {
		at = append(at, c)
	}

	slices.Sort(at)

	out := make([]run, 0, len(at))
	for i := 1; i < len(at); i++ {
		out = append(out, run{from: at[i-1], to: at[i], class: classAt(spans, at[i-1])})
	}

	return out
}

// under puts a face over a band, and leaves it alone where there is none: a
// context line has no side, and painting one behind it would draw the frame's
// own color over whatever the terminal was given.
func under(face lipgloss.Style, band color.Color, banded bool) lipgloss.Style {
	if !banded {
		return face
	}

	return face.Background(band)
}

func covered(spans []diff.Span, at int) bool {
	for _, s := range spans {
		if at >= s.From && at < s.To {
			return true
		}
	}

	return false
}

func classAt(spans []highlight.Span, at int) highlight.Class {
	for _, s := range spans {
		if at >= s.From && at < s.To {
			return s.Class
		}
	}

	return highlight.Plain
}

func refOf(path string, l diff.Line) diff.LineRef {
	return diff.LineRef{Path: path, Kind: l.Kind, Old: l.Old, New: l.New}
}

// spansFor is the grammar's reading of one line, lexed with the rest of its
// hunk the first time the hunk is drawn and kept after that.
//
// A hunk rather than a file is what there is to lex: the patch carries no more
// than that, which is also what makes the rich renderer work on a review
// prepared with no checkout. The state above the hunk is what it loses, and the
// caveat says so.
func (m *Model) spansFor(r row) []highlight.Span {
	at := hunkAt{path: r.path, hunk: r.hunk}

	read, ok := m.lexed[at]
	if !ok {
		read = m.lexHunk(at)
		m.lexed[at] = read
	}

	return read[refOf(r.path, r.line)]
}

// lexHunk lexes both sides of a hunk as whole texts and hands each line back
// its own spans. The two sides are lexed apart because a patch interleaves
// them, and a removed line followed by the line that replaced it is not a
// program either side of it would recognize.
func (m *Model) lexHunk(at hunkAt) map[diff.LineRef][]highlight.Span {
	before, after := m.diff.Sides(at.path, at.hunk)
	lexedBefore, lexedAfter := highlight.Lines(at.path, before), highlight.Lines(at.path, after)

	out := map[diff.LineRef][]highlight.Span{}
	oldAt, newAt := 0, 0

	for i := range m.diff.Files {
		if filePath(&m.diff.Files[i]) != at.path {
			continue
		}

		for _, l := range m.diff.Files[i].Lines {
			if l.Hunk != at.hunk {
				continue
			}

			switch l.Kind {
			case diff.KindRemove:
				out[refOf(at.path, l)] = spanAt(lexedBefore, oldAt)
				oldAt++
			case diff.KindAdd:
				out[refOf(at.path, l)] = spanAt(lexedAfter, newAt)
				newAt++
			default:
				out[refOf(at.path, l)] = spanAt(lexedAfter, newAt)
				oldAt++
				newAt++
			}
		}
	}

	return out
}

func spanAt(lexed [][]highlight.Span, at int) []highlight.Span {
	if at < 0 || at >= len(lexed) {
		return nil
	}

	return lexed[at]
}

// blend makes a band from a background and an accent: the accent's hue at the
// background's depth, lifted toward the middle by share.
//
// It keeps the hue rather than mixing the two colors, because a mix of a dark
// background and an accent lands in the grey ramp, where a 256-color terminal
// quantizes added and removed to the same slot and the band stops saying which
// side the line is on.
func blend(base, accent color.Color, at depth) color.Color {
	from, _ := colorful.MakeColor(base)
	to, _ := colorful.MakeColor(accent)

	hue, sat, _ := to.Hsl()
	_, _, dark := from.Hsl()

	const mid = 0.5

	return colorful.Hsl(hue, sat*at.sat, dark+(mid-dark)*at.lift).Clamped()
}

// cycleRenderer moves v on and says what the new one cannot do. A spike is
// worth living with only if its limits are in front of the person living with
// it, so the caveat is the status rather than a line in the help nobody opens.
func (m *Model) cycleRenderer() {
	m.drawn = m.drawn.next()

	if caveat := m.drawn.caveat(); caveat != "" {
		m.say(m.drawn.String()+": "+caveat, false)

		return
	}

	m.say(m.drawn.String(), false)
}
