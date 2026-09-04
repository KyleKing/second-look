package rate

import (
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/structure"
)

// Size is how many lines a change asks somebody to read. It is not the rating
// and never orders anything: it is the one thing a reader can check the rating
// against, since a 9 on eight hundred lines and a 9 on nine are different
// claims.
type Size struct {
	Added, Removed int
}

// Measure counts the lines of every hunk that changed code. A hunk the layout
// comparison settled counts toward neither side, so a re-indent over forty
// files measures nothing.
func Measure(d *diff.Diff, readings []structure.Reading, refs []seen.Ref) Size {
	var s Size

	for i := range readings {
		if readings[i].Change.Cosmetic() || i >= len(refs) {
			continue
		}

		added, removed := d.Changed(refs[i].Path, refs[i].Hunk)
		s.Added += added
		s.Removed += removed
	}

	return s
}
