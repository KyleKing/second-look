package tui

import (
	"fmt"
	"image/color"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/kyleking/aragonite/tui/theme"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/highlight"
)

// look is how a line of the diff is drawn. Side by side and the grammar are
// independent questions, so each is a toggle of its own: `u` turns one on or
// off and `v` walks the combinations worth having.
type look struct {
	// rich is the grammar in color under a band saying which side a line is on,
	// the runs that actually changed marked inside it, and both line numbers in
	// the gutter. Off, a line is a whole-line color and one number, which is
	// what a terminal diff has always looked like.
	rich bool
	// split puts each removal beside the addition that replaced it. It is the
	// one toggle that changes which rows exist rather than only how they are
	// drawn.
	split bool
	// wrap folds a line longer than the frame onto continuation lines rather
	// than cutting it with an ellipsis, for the diff whose long lines are the
	// point. The row still counts as one, so no motion changes and the cursor
	// bar sits on the line's first drawn line.
	wrap bool
}

// opening is what a review opens on, which is the one look the title does not
// name.
var opening = look{rich: true}

// presets are the combinations v walks, in the order it walks them. A look
// reached by toggling rather than by cycling is in none of them, and the next v
// starts the walk again rather than guessing where in it that look belongs.
func presets() []look {
	return []look{
		{rich: true},
		{rich: true, split: true},
	}
}

func (l look) next() look {
	all := presets()

	next := all[0]

	for i, p := range all {
		if p == l.withoutWrap() {
			next = all[(i+1)%len(all)]

			break
		}
	}

	// Wrap is a question of its own rather than one of the presets the cycle
	// walks, so it rides along on whichever look the cycle lands on.
	next.wrap = l.wrap

	return next
}

// withoutWrap is the look the presets are compared against, since none of
// them names wrap.
func (l look) withoutWrap() look {
	l.wrap = false

	return l
}

func (l look) String() string {
	var on []string

	if !l.rich {
		on = append(on, "plain")
	}

	if l.split {
		on = append(on, "split")
	}

	if l.wrap {
		on = append(on, "wrap")
	}

	if len(on) == 0 {
		return "rich"
	}

	return strings.Join(on, " ")
}

// caveat is what the mode does not do, said where the mode is named. A spike
// whose limits are only in the commit message is one nobody can judge.
func (l look) caveat() string {
	switch {
	case l.split:
		return "narrower than " + strconv.Itoa(splitWidth) + " columns it draws unified"
	case l.rich:
		return "a diff is a fragment, so a grammar's state above the file's first hunk is lost"
	}

	return ""
}

// splitWidth is the frame a side-by-side needs before each half is worth
// reading. Under it the mode draws unified rather than wrapping two columns of
// code into illegibility, and the caveat says so.
const splitWidth = 120

// sideBySide reports whether the screen is actually drawing two columns, which
// the mode asks for and the frame can refuse.
//
// The comment and code views are flattenings of their own, and a pairing that
// changed which rows exist under them would be a fourth and a fifth screen. So
// the split applies to the diff, which is the view it is about.
func (m *Model) sideBySide() bool {
	return m.drawn.split && m.view == viewDiff && m.diffWidth() >= splitWidth
}

// The two depths a side is drawn at: the band under a whole changed line, and
// the runs inside it that actually differ from the line it is paired with. The
// text is colored by grammar, so the side has to be said by something that
// leaves the foreground alone.
//
// The band is a hint rather than a wash, the way delta and GitHub draw it:
// barely off the frame's own color, with the mark a clear step deeper so the
// runs that changed are the loudest thing on the line. Deepening either one
// past this trades the code on it for the band, which is the wrong side of
// the line for a screen that exists to be read.
//
// A 256-color terminal gets deeper values, since the cube carries almost no
// dark tints: held back there, added and removed both quantize into the grey
// ramp and the band says nothing.
const (
	// What a terminal that mixes its own colors gets.
	trueBandLift, trueBandSat = 0.035, 0.45
	trueMarkLift, trueMarkSat = 0.28, 0.70
	// What a 256-color terminal gets, deep enough to survive the cube.
	cubeBandLift, cubeMarkLift = 0.14, 0.34
	cubeSat                    = 1.0
)

var (
	bandMix, markMix = depths(supportsMillions())

	trueBand = theme.Depth{Lift: trueBandLift, Sat: trueBandSat}
	trueMark = theme.Depth{Lift: trueMarkLift, Sat: trueMarkSat}
	cubeBand = theme.Depth{Lift: cubeBandLift, Sat: cubeSat}
	cubeMark = theme.Depth{Lift: cubeMarkLift, Sat: cubeSat}
)

func depths(millions bool) (theme.Depth, theme.Depth) {
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
	// side is the green or red a line's side is said with: its sign and the
	// line number the side carries, the way delta and GitHub color them rather
	// than inverted.
	side    map[byte]lipgloss.Style
	gutter  lipgloss.Style
	colored bool
}

func newRichStyles(s styles) richStyles {
	p, base := s.palette, lipgloss.NewStyle()

	faces := map[highlight.Class]lipgloss.Style{
		highlight.Comment:     base.Foreground(p.Overlay1).Italic(true),
		highlight.Keyword:     base.Foreground(p.Mauve),
		highlight.String:      base.Foreground(p.Yellow),
		highlight.Number:      base.Foreground(p.Peach),
		highlight.Function:    base.Foreground(p.Blue),
		highlight.Type:        base.Foreground(p.Teal),
		highlight.Name:        base.Foreground(p.Text),
		highlight.Punctuation: base.Foreground(p.Overlay2),
		highlight.Plain:       base.Foreground(p.Text),
	}

	return richStyles{
		class: faces,
		side: map[byte]lipgloss.Style{
			diff.KindAdd:    base.Foreground(p.Green),
			diff.KindRemove: base.Foreground(p.Red),
		},
		band: map[byte]color.Color{
			diff.KindAdd:    theme.Tint(p.Base, p.Green, bandMix),
			diff.KindRemove: theme.Tint(p.Base, p.Red, bandMix),
		},
		mark: map[byte]color.Color{
			diff.KindAdd:    theme.Tint(p.Base, p.Green, markMix),
			diff.KindRemove: theme.Tint(p.Base, p.Red, markMix),
		},
		gutter:  base.Foreground(p.Overlay0),
		colored: colorprofile.Detect(os.Stdout, os.Environ()) > colorprofile.ASCII,
	}
}

// richCode draws one line of the diff: its number, its side mark, and the
// text colored by grammar under the band for its side, with the runs that
// differ from the line it is paired with marked. One answer is one frame
// line; a wrapped line is as many as its text takes, and the continuation
// lines keep the band so they read as the same line rather than as new rows.
func (m *Model) richCode(r row, width int) []string {
	room := max(0, width-m.gutterWidth())
	band := m.bandRoom(r.path, r.line.Hunk, room)
	segs := m.richSegments(r, r.line, room)

	out := make([]string, len(segs))
	for i, seg := range segs {
		seg = m.fill(r, r.line, seg, band, room)

		gutter := m.padTo(r, r.line, m.gutterWidth())
		if i == 0 {
			gutter = m.richGutter(r)
		}

		out[i] = gutter + cut(seg, room)
	}

	return out
}

// rule closes the gutter, so the code has a left edge to run down.
const rule = "\u2502"

// gutterWidth is the number, and the side mark with a space on either side.
func (m *Model) gutterWidth() int {
	const markAndSpaces = 3

	return m.screen.numWidth + markAndSpaces
}

// richGutter is the number a line carries and its side mark. A side line's number
// and mark take its side's color, and the mark is the sign where color is gone.
func (m *Model) richGutter(r row) string {
	l := r.line
	num, style := l.New, m.rich.gutter
	if l.Kind == diff.KindRemove {
		num = l.Old
	}

	side, changed := m.rich.side[l.Kind]
	mark := sideGlyph(l.Kind, m.rich.colored)

	switch {
	case r.shared && m.rich.colored:
		style, mark = side, m.styles.behind.Render(sharedGlyph)
	case changed:
		style = side
		mark = side.Render(mark)
	}

	return style.Render(number(num, m.screen.numWidth)) + " " + mark + " "
}

// sharedGlyph marks a line a shape's file makes as the shape does.
const sharedGlyph = "≈"

// sideGlyph is the bar a changed line is marked with, or its sign where the terminal draws no color.
func sideGlyph(kind byte, colored bool) string {
	switch {
	case kind == diff.KindContext:
		return " "
	case colored:
		return "▎"
	default:
		return string(kind)
	}
}

// signCell is the line's +/-. The glyph stays, so a monochrome terminal loses
// the color and keeps the meaning.
func (m *Model) signCell(kind byte) string {
	style, ok := m.rich.side[kind]
	if !ok {
		return " "
	}

	return style.Render(string(kind))
}

func number(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}

	return fmt.Sprintf("%*d", width, n)
}

// padTo fills cells with the line's band where the diff pairs the line, and
// with spaces where it does not.
func (m *Model) padTo(r row, l diff.Line, cells int) string {
	spaces := strings.Repeat(" ", cells)

	band, ok := m.bandOf(r, l)
	if !ok {
		return spaces
	}

	return lipgloss.NewStyle().Background(band).Render(spaces)
}

// fill pads seg to room cells, with the band running to band cells and plain
// spaces after it.
func (m *Model) fill(r row, l diff.Line, seg string, band, room int) string {
	if over := band - textWidth(seg); over > 0 {
		seg += m.padTo(r, l, over)
	}

	if over := room - textWidth(seg); over > 0 {
		seg += strings.Repeat(" ", over)
	}

	return seg
}

// bandOf is the band a line is drawn under, which only a line the diff pairs
// with a replacement has. The row under the cursor tints that band toward the
// selection, and a line without one is lit in the selection itself.
func (m *Model) bandOf(r row, l diff.Line) (color.Color, bool) {
	_, paired := m.refined[refOf(r.path, l)]
	band, banded := m.rich.band[l.Kind]
	banded = banded && paired

	if !r.current {
		return band, banded
	}

	selection := m.styles.selection.GetBackground()
	if !banded {
		return selection, true
	}

	return theme.Mix(band, selection, selectedShare), true
}

// selectedShare is how far a band moves toward the selection under the cursor.
const selectedShare = 0.5

// bandMargin is the cells a band runs past the widest line of its hunk.
const bandMargin = 2

// bandRoom is where a hunk's bands end, so its lines end in one column rather
// than at their own lengths.
func (m *Model) bandRoom(path string, hunk, room int) int {
	return min(room, m.hunkCols[hunkRef{path: path, hunk: hunk}]+bandMargin)
}

type hunkRef struct {
	path string
	hunk int
}

// hunkWidths is the widest line of each hunk in cells.
func hunkWidths(d *diff.Diff) map[hunkRef]int {
	widths := map[hunkRef]int{}

	for i := range d.Files {
		path := pathOf(&d.Files[i])

		for _, l := range d.Files[i].Lines {
			ref := hunkRef{path: path, hunk: l.Hunk}
			widths[ref] = max(widths[ref], textWidth(expand(l.Text)))
		}
	}

	return widths
}

// richSegments is the line's text as styled segments no wider than width
// cells, cut into runs by grammar and by what changed, each run drawn with
// the color of its class over the background of its depth. One segment is the
// whole answer where wrap is off or the line fits; where wrap is on a longer
// line is as many segments as it takes, and a segment never straddles an
// escape because each piece is rendered on the line it lands on.
//
// The runs index the line as the patch spells it, so tabs are expanded run by
// run against a running column rather than up front: expanding first would
// move every byte offset the grammar and the pairing answered in.
func (m *Model) richSegments(r row, l diff.Line, width int) []string {
	band, ok := m.bandOf(r, l)
	mark := m.rich.mark[l.Kind]
	marked := m.refined[refOf(r.path, l)]

	var segs []string

	var b strings.Builder

	cols := 0

	// A hunk already read keeps its band and loses its grammar: what the code
	// says has been read, and what is left to read is the question.
	faces := m.rich.class
	if m.behind(r) {
		faces = nil
	}

	for _, piece := range m.runs(r, l) {
		face := m.styles.behind
		if faces != nil {
			face = faces[piece.class]
		}

		style := under(face, band, ok)
		if ok && covered(marked, piece.from) {
			style = under(face, mark, true)
		}

		text, spent := expandFrom(l.Text[piece.from:piece.to], cols)

		// With wrap off the whole run goes in and the caller's cut marks where
		// it ran out, so the check comes after the write.
		if !m.drawn.wrap {
			b.WriteString(style.Render(text))

			if cols = spent; cols >= width {
				break
			}

			continue
		}

		for text != "" {
			room := width - cols
			if room < 1 {
				segs = append(segs, b.String())
				b.Reset()
				cols, room = 0, width
			}

			part, rest := takeCells(text, room)
			b.WriteString(style.Render(part))
			cols += textWidth(part)
			text = rest
		}
	}

	return append(segs, b.String())
}

// takeCells is the first room cells of s and what is left, with at least one
// glyph taken however narrow room is, so a caller wrapping text cannot spin
// on a glyph wider than the line it is being wrapped to.
func takeCells(s string, room int) (string, string) {
	cols := 0

	for i, r := range s {
		w := textWidth(string(r))
		if cols > 0 && cols+w > room {
			return s[:i], s[i:]
		}

		cols += w
		if cols >= room {
			return s[:i+len(string(r))], s[i+len(string(r)):]
		}
	}

	return s, ""
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
func (m *Model) runs(r row, l diff.Line) []run {
	text := l.Text
	cuts := map[int]bool{0: true, len(text): true}

	spans := m.spansFor(r, l)
	for _, s := range spans {
		cuts[min(s.From, len(text))], cuts[min(s.To, len(text))] = true, true
	}

	for _, s := range m.refined[refOf(r.path, l)] {
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
// file the first time any of it is drawn and kept after that.
//
// The diff's copy of the file rather than the file itself is what there is to
// lex, which is also what makes the rich renderer work on a review prepared
// with no checkout. Lexing the whole file's hunks together lets the grammar
// carry state across them, so the only state lost is above the file's first
// hunk; the caveat says so.
func (m *Model) spansFor(r row, l diff.Line) []highlight.Span {
	if m.view == viewFile {
		return m.spansIn(l.New)
	}

	read, ok := m.lexed[r.path]
	if !ok {
		read = m.lexFile(r.path)
		m.lexed[r.path] = read
	}

	return read[refOf(r.path, l)]
}

// lexFile lexes both sides of a file's diff as whole texts and hands each line
// back its own spans. The two sides are lexed apart because a patch interleaves
// them, and a removed line followed by the line that replaced it is not a
// program either side of it would recognize.
func (m *Model) lexFile(path string) map[diff.LineRef][]highlight.Span {
	var file *diff.File
	for i := range m.diff.Files {
		if filePath(&m.diff.Files[i]) == path {
			file = &m.diff.Files[i]

			break
		}
	}
	if file == nil {
		return nil
	}

	var before, after []string
	for _, l := range file.Lines {
		switch l.Kind {
		case diff.KindRemove:
			before = append(before, l.Text)
		case diff.KindAdd:
			after = append(after, l.Text)
		default:
			before = append(before, l.Text)
			after = append(after, l.Text)
		}
	}

	lexedBefore, lexedAfter := highlight.Lines(path, before), highlight.Lines(path, after)

	out := map[diff.LineRef][]highlight.Span{}
	oldAt, newAt := 0, 0

	for _, l := range file.Lines {
		switch l.Kind {
		case diff.KindRemove:
			out[refOf(path, l)] = spanAt(lexedBefore, oldAt)
			oldAt++
		case diff.KindAdd:
			out[refOf(path, l)] = spanAt(lexedAfter, newAt)
			newAt++
		default:
			out[refOf(path, l)] = spanAt(lexedAfter, newAt)
			oldAt++
			newAt++
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

// cycleRenderer moves v on and says what the new one cannot do. A spike is
// worth living with only if its limits are in front of the person living with
// it, so the caveat is the status rather than a line in the help nobody opens.
func (m *Model) cycleRenderer() {
	m.drawn = m.drawn.next()

	// Side by side pairs a removal with the addition that replaced it, which
	// changes which rows exist rather than only how they are drawn, so the
	// screen is laid out again rather than merely redrawn.
	m.rebuild()
	m.sayLook()
}

// toggleLook flips one of the questions on its own, which is what the cycle
// cannot do: reading a wide diff side by side without the grammar.
func (m *Model) toggleLook(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "g":
		m.drawn.rich = !m.drawn.rich
	case "s":
		m.drawn.split = !m.drawn.split
	case "w":
		m.drawn.wrap = !m.drawn.wrap
	default:
		m.say("nothing to draw for "+msg.String()+
			"; g grammar, s side by side, w wrap", false)

		return
	}

	m.rebuild()
	m.sayLook()
}

func (m *Model) sayLook() {
	if caveat := m.drawn.caveat(); caveat != "" {
		m.say(m.drawn.String()+": "+caveat, false)

		return
	}

	m.say(m.drawn.String(), false)
}

// splitCode draws one row as two columns: what the line said before the change
// on the left, what it says now on the right.
//
// The divider is a column of its own so the two halves never touch, and a half
// with no line is drawn as its own empty band rather than as blank frame, since
// "nothing was here" and "the row ended" are different things.
func (m *Model) splitCode(r row, width int) []string {
	const divider = " │ "

	left, right := sidesOf(r)
	half := (width - textWidth(divider)) / sides

	// Each half numbers the file it is showing, so a context line carries its
	// pre-image number on the left and its post-image number on the right: the
	// two diverge the moment anything above them was added or removed.
	ls := m.halfCode(r, left, left.Old, half)
	rs := m.halfCode(r, right, right.New, width-half-textWidth(divider))

	out := make([]string, 0, max(len(ls), len(rs)))
	for i := range max(len(ls), len(rs)) {
		l, rt := strings.Repeat(" ", half), ""
		if i < len(ls) {
			l = ls[i]
		}
		if i < len(rs) {
			rt = rs[i]
		}

		out = append(out, l+m.rich.gutter.Render(divider)+rt)
	}

	return out
}

// sides is the two halves a split row is cut into.
const sides = 2

// halfCode is one column: the line's own number, then its text under its
// band. An absent line keeps the width and draws nothing, so the divider
// stays straight down the frame. Wrap takes a half to as many drawn lines as
// its text needs, and the halves are zipped by the caller.
func (m *Model) halfCode(r row, l diff.Line, at, width int) []string {
	const signAndSpaces = 3

	gutter := m.screen.numWidth + signAndSpaces

	if l.Kind == 0 {
		return []string{strings.Repeat(" ", width)}
	}

	room := max(0, width-gutter)
	head := m.rich.gutter.Render(number(at, m.screen.numWidth)+rule) +
		m.signCell(l.Kind) + " "

	segs := m.richSegments(r, l, room)
	out := make([]string, len(segs))

	for i, seg := range segs {
		seg = m.fill(r, l, seg, m.bandRoom(r.path, l.Hunk, room), room)

		lead := head
		if i > 0 {
			lead = strings.Repeat(" ", gutter)
		}

		out[i] = lead + cut(seg, room)
	}

	return out
}
