package diff

// Sides of a diff, spelled the way GitHub spells them.
const (
	SideRight = "RIGHT"
	SideLeft  = "LEFT"
)

// Anchor returns the diff line a comment at (path, side, line) points at.
// A RIGHT anchor numbers the post-image and a LEFT anchor the pre-image,
// which is GitHub's own convention.
//
// It reports false for a line the diff does not carry, which is the case
// worth catching: GitHub refuses a comment outside the diff, and a line
// number invented out of nothing lands there.
func (d *Diff) Anchor(path, side string, line int) (string, bool) {
	l, ok := d.lookup(path, side, line)

	return l.Text, ok
}

// HunkOf returns the number of the @@ block the line falls in, so a caller can
// tell whether two lines of a multi-line comment share one.
func (d *Diff) HunkOf(path, side string, line int) (int, bool) {
	l, ok := d.lookup(path, side, line)

	return l.Hunk, ok
}

// Relocate finds the line at (path, side) whose text now matches exactly the
// text a comment was last staged against, for the case a comment's numbered
// line moved but the line itself did not change: an insertion elsewhere in
// the file shifted everything below it by a fixed offset. It reports false
// when the text is not found, or found more than once, since a comment
// resting on a duplicated line (a repeated blank, a common closing brace) has
// no single line to relocate to.
func (d *Diff) Relocate(path, side, text string) (int, bool) {
	number := func(l Line) int { return l.New }
	if side == SideLeft {
		number = func(l Line) int { return l.Old }
	}

	found := 0

	for i := range d.Files {
		f := &d.Files[i]
		if (side == SideLeft && f.OldPath != path) || (side != SideLeft && f.NewPath != path) {
			continue
		}

		for _, l := range f.Lines {
			if l.Text != text {
				continue
			}

			if n := number(l); n > 0 {
				if found > 0 {
					return 0, false
				}

				found = n
			}
		}
	}

	return found, found > 0
}

func (d *Diff) lookup(path, side string, line int) (Line, bool) {
	for i := range d.Files {
		f := &d.Files[i]

		number := func(l Line) int { return l.New }
		if side == SideLeft {
			number = func(l Line) int { return l.Old }
		}

		if (side == SideLeft && f.OldPath != path) || (side != SideLeft && f.NewPath != path) {
			continue
		}

		if l, ok := lineAt(f.Lines, line, number); ok {
			return l, true
		}
	}

	return Line{}, false
}

func lineAt(lines []Line, want int, number func(Line) int) (Line, bool) {
	for _, l := range lines {
		if number(l) == want {
			return l, true
		}
	}

	return Line{}, false
}
