package diff

// Piece is one run of two lines read as one: what both carry, what only the
// old line carried, or what only the new one does. From and To index the new
// text for KindContext and KindAdd, and the old text for KindRemove.
type Piece struct {
	Kind     byte
	From, To int
}

// Merge reads a paired removal and addition as one line: the new text in
// order, with the runs the old line lost set into the places they were cut
// from. It answers nil where the pair shares too little to read as one line —
// a rewritten line's removed runs scattered through its replacement are noise
// rather than an edit — and the caller then draws the two lines it has.
//
// The runs are the pairing's own vocabulary: tokens the lines share read as
// context, and each gap between them pairs the old side's cut with the new
// side's insert, trimmed to the bytes that differ the way the mark pass trims.
func Merge(before, after string) []Piece {
	if before == after {
		return nil
	}

	left, right := tokens(before), tokens(after)
	if len(left) > tokenCap || len(right) > tokenCap {
		return nil
	}

	keptLeft, keptRight := commonSpans(before, left, after, right)

	shared := 0
	for _, at := range keptLeft {
		shared += left[at].To - left[at].From
	}

	const sides = 2

	if mean := float64(len(before)+len(after)) / sides; mean == 0 ||
		float64(shared)/mean < sameEnough {
		return nil
	}

	var out []Piece

	li, ri := 0, 0

	// gap is one edit site: the old side's cut followed by the new side's
	// insert, with the bytes they share kept out of both. The prefix and
	// suffix sit where they belong in the new text, so the order a merged
	// line reads in is kept, cut, insert, kept.
	gap := func(goneTo, cameTo int) {
		var del, add Span
		if li < goneTo {
			del = Span{From: left[li].From, To: left[goneTo-1].To}
		}
		if ri < cameTo {
			add = Span{From: right[ri].From, To: right[cameTo-1].To}
		}

		pre := trimHead(before, del, after, add)
		// The tail trims what the head left: counting from both ends at once
		// would let a short gap's shared bytes be kept twice.
		suf := trimTail(before, del, after, add)
		suf = min(suf, min(del.To-del.From-pre, add.To-add.From-pre))

		if pre > 0 {
			out = append(out, Piece{Kind: KindContext, From: add.From, To: add.From + pre})
		}

		if del.From+pre < del.To-suf {
			out = append(out, Piece{Kind: KindRemove, From: del.From + pre, To: del.To - suf})
		}

		if add.From+pre < add.To-suf {
			out = append(out, Piece{Kind: KindAdd, From: add.From + pre, To: add.To - suf})
		}

		if suf > 0 {
			out = append(out, Piece{Kind: KindContext, From: add.To - suf, To: add.To})
		}

		li, ri = goneTo, cameTo
	}

	for k := range keptLeft {
		gap(keptLeft[k], keptRight[k])
		out = append(out, Piece{
			Kind: KindContext, From: right[keptRight[k]].From, To: right[keptRight[k]].To,
		})
		li, ri = keptLeft[k]+1, keptRight[k]+1
	}

	gap(len(left), len(right))

	return out
}

// trimHead is how many leading bytes of a paired cut and insert are the same,
// which is what keeps a rename's shared affix out of the mark.
func trimHead(before string, del Span, after string, add Span) int {
	n := 0
	for del.From+n < del.To && add.From+n < add.To &&
		before[del.From+n] < 0x80 && before[del.From+n] == after[add.From+n] {
		n++
	}

	return n
}

// trimTail is trimHead from the other end.
func trimTail(before string, del Span, after string, add Span) int {
	n := 0
	for del.From < del.To-n && add.From < add.To-n &&
		before[del.To-1-n] < 0x80 && before[del.To-1-n] == after[add.To-1-n] {
		n++
	}

	return n
}
