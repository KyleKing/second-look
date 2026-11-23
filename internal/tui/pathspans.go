package tui

import (
	"path"

	"github.com/kyleking/aragonite/tui/icons"
)

// pathSpans is a file's heading name: its type's icon, the directory a step
// back, and the file's own name at full weight, so the eye lands on the part
// that differs between two rows. Read end to end it is still the path.
func pathSpans(p string, set icons.Set) []chunk {
	dir, name := path.Split(p)
	out := make([]chunk, 0, 3)

	if g := set.File(p); g.Text != "" {
		out = append(out, iconSpan(g))
	}

	if dir != "" {
		out = append(out, muted(dir))
	}

	return append(out, chunk{text: name})
}
