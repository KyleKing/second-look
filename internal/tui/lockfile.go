package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/meta"
)

// shutHead is a folded file's heading: what it holds, whatever can be asked of
// it, and the key that shows its lines.
func shutHead(head row, words []string, r *artifact.Review, hints ...chunk) row {
	if n := staged(r, head.path); n != "" {
		words = append(words, n)
	}

	head = spanned(head, headed(chunk{text: head.path}, words, append(hints, rawHint)...)...)
	head.folded = true

	return head
}

// foldedFile is a file drawn as one row, plus whatever it can say without being
// opened: the dependencies a lockfile changed where the format is readable, and
// a hunk count where it is not.
func foldedFile(f *diff.File, head row, c fileCtx) []row {
	word, deps := lockRows(f, head.path, c.lay)

	if word == "" {
		head = shutHead(head, []string{plural(hunkCount(f), "hunk")}, c.r)
	} else {
		asking, ask := askedWords(f, c.lay.answered[head.path])
		words := append([]string{word}, asking...)
		words = append(words, versionedWords(c.lay.versioned[head.path])...)
		head = shutHead(head, words, c.r, ask...)
	}

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
func lockRows(f *diff.File, path string, lay layout) (string, []row) {
	deps, ok := meta.Read(f)
	if !ok || len(deps) == 0 {
		return "", nil
	}

	width := 0
	for _, d := range deps {
		width = max(width, lipgloss.Width(d.Name))
	}

	eco := meta.Ecosystem(f)
	out := make([]row, 0, len(deps))

	for _, d := range deps {
		card, has := lay.cards[advisory.Package{Ecosystem: eco, Name: d.Name, Version: d.To}]
		out = append(out, spanned(row{kind: rowHunk, path: path, comment: noComment},
			chunk{text: fmt.Sprintf("%-*s", width, d.Name), face: faceBold},
			chunk{text: "  " + versionWord(d), face: faceBody},
			muted(cardWord(card, has, eco)),
		))

		if detail := cardDetail(card); has && detail != "" {
			out = append(out, row{
				kind: rowHunk, path: path, comment: noComment,
				text: fmt.Sprintf("%-*s  %s", width, "", detail),
			})
		}

		for _, n := range lay.known[advisory.Package{Ecosystem: eco, Name: d.Name, Version: d.To}] {
			out = append(out, row{
				kind: rowHunk, path: path, comment: noComment,
				text: fmt.Sprintf("%-*s  %s", width, "", advisoryWord(n)),
			})
		}
	}

	return humanize.Plural(len(deps), "dependency", "dependencies"), out
}

// askedWords is what the lockfile's own row says about the advisories, because
// a question still out, a question refused, and an answer of nothing all draw
// the same rows and mean different things. Before any asking it is the key.
func askedWords(f *diff.File, state asked) ([]string, []chunk) {
	switch {
	case state.out:
		return []string{"asking osv.dev…"}, nil
	case state.failed != "":
		return []string{"osv.dev: " + state.failed}, nil
	case state.settled && state.found > 0:
		return []string{plural(state.found, "package") + " with advisories"}, nil
	case state.settled:
		return []string{"no advisories"}, nil
	case meta.Ecosystem(f) == "":
		return nil, nil
	}

	return nil, []chunk{hintSpan("L", "advisories")}
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
