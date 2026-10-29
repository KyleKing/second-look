package diff

import (
	"strconv"
	"strings"
)

// Span is a half-open byte range of a line's text.
type Span struct {
	From int
	To   int
}

// LineRef names one line of a diff. The pair of numbers is enough on its own: a
// removed line carries only Old and an added line only New, so no two lines of
// one file share a ref.
type LineRef struct {
	Path string
	Kind byte
	Old  int
	New  int
}

// Refined is the runs of each changed line that its partner does not carry. A
// line absent from it changed as a whole, which is what the line's own color
// already says.
type Refined map[LineRef][]Span

// Refine pairs the removed and added lines of every hunk and reports which runs
// of each differ, so a one-word edit reads as one word rather than as two
// entirely changed lines.
//
// Pairing is by best match inside a change block, which is a run of removals
// followed by a run of additions: each removal answers the addition most like
// it, never crossing another pair. An unpaired line is left whole, and so is a
// pair with too little in common, since marking most of a rewritten line is
// noise rather than information.
func (d *Diff) Refine() Refined { return d.pair().marks }

// Reindented is every changed line paired with one saying the same thing at
// another indent, which is a block gaining or losing a level around it.
func (d *Diff) Reindented() map[LineRef]bool { return d.pair().reindented }

// Paired is the pairing in one pass: the changed runs, the re-indented lines,
// and the moved runs, which Refine, Reindented, and Moved each answer alone.
// A caller wanting all three asks once, because the pairing is the expensive
// read a diff offers.
func (d *Diff) Paired() (Refined, map[LineRef]bool, map[LineRef]Move) {
	p := d.pair()

	return p.marks, p.reindented, p.moved
}

type pairing struct {
	marks      Refined
	reindented map[LineRef]bool
	// moved lines answer a run elsewhere, so they pair with nothing in their own block.
	moved map[LineRef]Move
}

func (d *Diff) pair() pairing {
	out := pairing{marks: Refined{}, reindented: map[LineRef]bool{}, moved: d.Moved()}

	for i := range d.Files {
		refineFile(out, pathOf(&d.Files[i]), d.Files[i].Lines)
	}

	return out
}

func refineFile(out pairing, path string, lines []Line) {
	var gone, came []Line

	flush := func() {
		gone, came = refineReindented(out, path, gone, came)
		align(out, path, gone, came)

		gone, came = nil, nil
	}

	for _, l := range lines {
		if _, ok := out.moved[refOf(path, l)]; ok {
			continue
		}

		switch l.Kind {
		case KindRemove:
			// An addition already seen closes the block, so what follows opens
			// a new one rather than pairing across an unrelated boundary.
			if len(came) > 0 {
				flush()
			}

			gone = append(gone, l)
		case KindAdd:
			came = append(came, l)
		default:
			flush()
		}
	}

	flush()
}

// refineReindented pairs a block's lines that say the same thing at another indent,
// so a line pushed under a new `if` does not pair with the `if` by position.
func refineReindented(out pairing, path string, gone, came []Line) ([]Line, []Line) {
	if len(gone) == 0 || len(came) == 0 || len(gone)*len(came) > blockCap {
		return gone, came
	}

	left, right := make([]string, len(gone)), make([]string, len(came))
	for i, l := range gone {
		left[i] = indentKey(l.Text, "-", i)
	}

	for i, l := range came {
		right[i] = indentKey(l.Text, "+", i)
	}

	atLeft, atRight := common(left, right)
	if len(atLeft) == 0 {
		return gone, came
	}

	paired := map[int]bool{}

	for k := range atLeft {
		g, c := gone[atLeft[k]], came[atRight[k]]
		refineOne(out, path, g, c)
		paired[atLeft[k]] = true

		if g.Text != c.Text {
			out.reindented[refOf(path, g)], out.reindented[refOf(path, c)] = true, true
		}
	}

	restGone := make([]Line, 0, len(gone)-len(atLeft))

	for i, l := range gone {
		if !paired[i] {
			restGone = append(restGone, l)
		}
	}

	clear(paired)

	for _, at := range atRight {
		paired[at] = true
	}

	restCame := make([]Line, 0, len(came)-len(atRight))

	for i, l := range came {
		if !paired[i] {
			restCame = append(restCame, l)
		}
	}

	return restGone, restCame
}

// blockCap bounds the line pairing, which is quadratic in a block's lines.
const blockCap = 160_000

// indentKey is a line's text without its indent, or a key nothing matches
// where that text is too short to mean anything.
func indentKey(text, side string, at int) string {
	trimmed := strings.TrimLeft(text, " \t")
	if len(strings.TrimSpace(trimmed)) < indentFloor {
		return side + strconv.Itoa(at) + "\x00"
	}

	return trimmed
}

// indentFloor is the shortest text a re-indented line is matched on, so a lone
// brace or `)` does not pair across a block.
const indentFloor = 4

func refineOne(out pairing, path string, gone, came Line) {
	before, after, ok := mark(gone.Text, came.Text)
	if !ok {
		return
	}

	out.marks[refOf(path, gone)] = before
	out.marks[refOf(path, came)] = after
}

// align pairs a block's removals with its additions by best match rather than
// by position, so a prose edit that merged two lines into one still pairs the
// line it kept instead of showing three whole-line changes. Pairs may not
// cross: two lines that traded places are a move, which pair() has already
// taken out of the block.
func align(out pairing, path string, gone, came []Line) {
	if len(gone) == 0 || len(came) == 0 {
		return
	}

	if len(gone)*len(came) > blockCap {
		// The match table is quadratic in the block, which the same cap as the
		// reindent pairing bounds. Past it the positional pairing is what a
		// huge generated block gets.
		for at := range min(len(gone), len(came)) {
			refineOne(out, path, gone[at], came[at])
		}

		return
	}

	for _, pair := range matched(score(gone, came)) {
		refineOne(out, path, gone[pair[0]], came[pair[1]])
	}
}

// score weighs every removal against every addition on their shared tokens
// over the mean length, and floors out the pairs too unlike to mark.
func score(gone, came []Line) [][]float64 {
	left, right := make([]map[string]int, len(gone)), make([]map[string]int, len(came))
	for i, l := range gone {
		left[i] = bagOf(l.Text)
	}

	for i, l := range came {
		right[i] = bagOf(l.Text)
	}

	out := make([][]float64, len(gone))
	cells := make([]float64, len(gone)*len(came))

	for i := range gone {
		out[i] = cells[i*len(came) : (i+1)*len(came)]

		for j := range came {
			mean := float64(len(gone[i].Text)+len(came[j].Text)) / halves
			if mean > 0 {
				out[i][j] = float64(shared(left[i], right[j])) / mean
			}
			if out[i][j] < sameEnough {
				out[i][j] = 0
			}
		}
	}

	return out
}

// matched walks back the best non-crossing set of pairs a score table offers,
// which is a longest-subsequence alignment scored on lines instead of tokens.
func matched(score [][]float64) [][2]int {
	cols := len(score[0]) + 1
	best := make([]float64, (len(score)+1)*cols)

	at := func(i, j int) float64 { return best[i*cols+j] }
	set := func(i, j int, v float64) { best[i*cols+j] = v }

	for i := 1; i <= len(score); i++ {
		for j := 1; j <= len(score[0]); j++ {
			set(i, j, max(
				max(at(i-1, j), at(i, j-1)),
				at(i-1, j-1)+score[i-1][j-1],
			))
		}
	}

	var out [][2]int

	for i, j := len(score), len(score[0]); i > 0 && j > 0; {
		switch at(i, j) {
		case at(i-1, j):
			i--
		case at(i, j-1):
			j--
		default:
			if score[i-1][j-1] > 0 {
				out = append(out, [2]int{i - 1, j - 1})
			}

			i--
			j--
		}
	}

	return out
}

// halves is the count a mean of two lengths divides by.
const halves = 2

// bagOf is a line's tokens as a multiset, which is what a match is weighed on
// before the costlier token-sequence check decides the pair.
func bagOf(text string) map[string]int {
	out := map[string]int{}
	for _, s := range tokens(text) {
		if strings.TrimSpace(text[s.From:s.To]) == "" {
			continue
		}

		out[text[s.From:s.To]]++
	}

	return out
}

// shared is how many bytes of token two lines carry in common, unordered: an
// upper bound on what an ordered match can find, so pairs that could never
// reach the floor are not asked to.
func shared(a, b map[string]int) int {
	if len(a) > len(b) {
		a, b = b, a
	}

	total := 0
	for token, n := range a {
		total += min(n, b[token]) * len(token)
	}

	return total
}

func refOf(path string, l Line) LineRef {
	return LineRef{Path: path, Kind: l.Kind, Old: l.Old, New: l.New}
}

// tokenCap bounds the pairing, which is quadratic in the tokens of a line. Past
// it the line is a minified bundle or a data blob, where a word-level mark says
// nothing a reader can use anyway.
const tokenCap = 400

// sameEnough is how much of the two lines has to be shared before a word-level
// mark is drawn instead of the whole line. It is measured against their mean
// length rather than the longer, so a line that gained a clause is still marked
// while two lines that merely share their indent are not.
const sameEnough = 0.4

// mark reports the runs of each line the other does not carry, and false where
// the two have too little in common to pair.
func mark(before, after string) ([]Span, []Span, bool) {
	if before == after {
		return nil, nil, false
	}

	left, right := tokens(before), tokens(after)
	if len(left) > tokenCap || len(right) > tokenCap {
		return nil, nil, false
	}

	keptLeft, keptRight := common(texts(before, left), texts(after, right))

	shared := 0
	for _, at := range keptLeft {
		shared += left[at].To - left[at].From
	}

	const sides = 2

	if mean := float64(len(before)+len(after)) / sides; mean == 0 ||
		float64(shared)/mean < sameEnough {
		return nil, nil, false
	}

	return gaps(left, keptLeft), gaps(right, keptRight), true
}

// tokens splits a line into the units an edit is measured in: a run of word
// bytes, a run of spaces, or one byte of anything else. Splitting on whitespace
// alone marks `f(a, b)` as wholly changed when only `b` moved.
func tokens(s string) []Span {
	var out []Span

	for at := 0; at < len(s); {
		end := at + 1
		for end < len(s) && sameClass(s[at], s[end]) {
			end++
		}

		out = append(out, Span{From: at, To: end})
		at = end
	}

	return out
}

func sameClass(a, b byte) bool {
	if wordByte(a) && wordByte(b) {
		return true
	}

	return a == ' ' && b == ' '
}

// wordByte reports whether a byte joins a word run. Every byte of a multi-byte
// rune is over 0x7f and joins one, which keeps an identifier in a non-Latin
// script a single token rather than one per byte.
func wordByte(c byte) bool {
	return c >= 0x80 || c == '_' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// common is the longest subsequence of tokens both lines carry in order,
// reported as the index of each match on either side.
func common(left, right []string) ([]int, []int) {
	// table[i*cols+j] is the length of the longest common subsequence of the
	// suffixes starting at left[i] and right[j]. It is one flat slice rather
	// than a row per i, because a line's pairing calls this once per block and
	// the row allocations were the pairing's own tax.
	cols := len(right) + 1
	table := make([]int, (len(left)+1)*cols)
	at := func(i, j int) int { return table[i*cols+j] }
	set := func(i, j, v int) { table[i*cols+j] = v }

	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				set(i, j, at(i+1, j+1)+1)

				continue
			}

			set(i, j, max(at(i+1, j), at(i, j+1)))
		}
	}

	var atLeft, atRight []int

	for i, j := 0, 0; i < len(left) && j < len(right); {
		switch {
		case left[i] == right[j]:
			atLeft = append(atLeft, i)
			atRight = append(atRight, j)
			i++
			j++
		case at(i+1, j) >= at(i, j+1):
			i++
		default:
			j++
		}
	}

	return atLeft, atRight
}

// gaps is every token not in kept, merged into runs, which is what a reader
// sees marked.
func gaps(all []Span, kept []int) []Span {
	held := make([]bool, len(all))
	for _, at := range kept {
		held[at] = true
	}

	var out []Span

	for i, s := range all {
		if held[i] {
			continue
		}

		if n := len(out); n > 0 && out[n-1].To == s.From {
			out[n-1].To = s.To

			continue
		}

		out = append(out, s)
	}

	return out
}

func texts(s string, spans []Span) []string {
	out := make([]string, len(spans))
	for i, at := range spans {
		out[i] = s[at.From:at.To]
	}

	return out
}
