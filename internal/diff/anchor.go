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
	for i := range d.Files {
		f := &d.Files[i]

		if side == SideLeft {
			if f.OldPath != path {
				continue
			}

			if text, ok := lineAt(f.Lines, line, func(l Line) int { return l.Old }); ok {
				return text, true
			}

			continue
		}

		if f.NewPath != path {
			continue
		}

		if text, ok := lineAt(f.Lines, line, func(l Line) int { return l.New }); ok {
			return text, true
		}
	}

	return "", false
}

func lineAt(lines []Line, want int, number func(Line) int) (string, bool) {
	for _, l := range lines {
		if number(l) == want {
			return l.Text, true
		}
	}

	return "", false
}
