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
