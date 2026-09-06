package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/meta"
)

// foldedFile is a file drawn as one row, plus whatever it can say without being
// opened: the dependencies a lockfile changed where the format is readable, and
// a hunk count where it is not.
func foldedFile(f *diff.File, head row, c fileCtx) []row {
	word, deps := lockRows(f, head.path)
	if word == "" {
		word = plural(hunkCount(f), "hunk") + " folded"
	}

	head.text = head.path + "  " + word + staged(c.r, head.path) + " · za to open"
	head.folded = true

	for _, ln := range f.Lines {
		claim(c.byLine, c.placed, head.path, ln)
	}

	return append([]row{head}, deps...)
}

// lockRows is what a lockfile did to its dependencies, drawn where the hashes
// themselves would be folded away to a hunk count that says nothing.
//
// The heading word comes back with it, because a file whose format is not
// readable here has to keep counting hunks.
func lockRows(f *diff.File, path string) (string, []row) {
	deps, ok := meta.Read(f)
	if !ok || len(deps) == 0 {
		return "", nil
	}

	width := 0
	for _, d := range deps {
		width = max(width, lipgloss.Width(d.Name))
	}

	out := make([]row, 0, len(deps))

	for _, d := range deps {
		out = append(out, row{
			kind: rowHunk, path: path, comment: noComment,
			text: fmt.Sprintf("%-*s  %s", width, d.Name, versionWord(d)),
		})
	}

	return humanize.Plural(len(deps), "dependency", "dependencies"), out
}

func versionWord(d meta.Row) string {
	switch {
	case d.From == "":
		return "added " + d.To
	case d.To == "":
		return "removed " + d.From
	}

	return d.From + " → " + d.To
}
