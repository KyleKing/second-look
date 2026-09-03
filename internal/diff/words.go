package diff

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
// Pairing is positional inside a change block, which is a run of removals
// followed by a run of additions: the first removal answers the first addition.
// An unpaired line is left whole, and so is a pair with too little in common,
// since marking most of a rewritten line is noise rather than information.
func (d *Diff) Refine() Refined {
	out := Refined{}

	for i := range d.Files {
		refineFile(out, pathOf(&d.Files[i]), d.Files[i].Lines)
	}

	return out
}

func refineFile(out Refined, path string, lines []Line) {
	var gone, came []Line

	flush := func() {
		for at := range min(len(gone), len(came)) {
			refineOne(out, path, gone[at], came[at])
		}

		gone, came = nil, nil
	}

	for _, l := range lines {
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

func refineOne(out Refined, path string, gone, came Line) {
	before, after, ok := mark(gone.Text, came.Text)
	if !ok {
		return
	}

	out[LineRef{Path: path, Kind: gone.Kind, Old: gone.Old, New: gone.New}] = before
	out[LineRef{Path: path, Kind: came.Kind, Old: came.Old, New: came.New}] = after
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
	// table[i][j] is the length of the longest common subsequence of the
	// suffixes starting at left[i] and right[j].
	table := make([][]int, len(left)+1)
	for i := range table {
		table[i] = make([]int, len(right)+1)
	}

	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				table[i][j] = table[i+1][j+1] + 1

				continue
			}

			table[i][j] = max(table[i+1][j], table[i][j+1])
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
		case table[i+1][j] >= table[i][j+1]:
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
