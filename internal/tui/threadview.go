package tui

import (
	"fmt"
	"sort"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
)

// buildThreads is every open conversation on the pull request and nothing else.
//
// GitHub's own list is unreadable for the reason the conversation queue exists:
// it interleaves the diff, so what is still being asked spreads through a page
// of code nobody is rereading. Here each thread carries the one line it anchors
// to and the rest of the diff is left out, which is the density the queue
// already has and the anchor the queue cannot show.
func buildThreads(d *diff.Diff, ts []threads.Thread, lay layout) screen {
	s := screen{numWidth: numberWidth(d)}
	at := anchorLines(d)

	for _, path := range threadPaths(ts) {
		if len(s.rows) > 0 {
			s.rows = append(s.rows, row{kind: rowBlank, comment: noComment})
		}

		s.rows = append(s.rows, row{
			kind: rowFile, path: path, comment: noComment,
			text: fmt.Sprintf("%s  %s", path, plural(countThreads(ts, path), "conversation")),
		})

		for i := range ts {
			if ts[i].Path != path {
				continue
			}

			s.rows = append(s.rows, quoted(&ts[i], at)...)
			s.rows = append(s.rows, threadRows(&ts[i], i, path, s.numWidth, lay, anchorWord(&ts[i]))...)
		}
	}

	if len(s.rows) == 0 {
		s.rows = append(s.rows, row{
			kind: rowFile, comment: noComment, text: "no open conversations on this pull request",
		})
	}

	return s
}

// quoted is the line a thread hangs from, drawn above it as a line of the diff
// so it carries the same gutter and grammar it does everywhere else. A view of
// prose about code nobody can see is the failure this exists to fix.
func quoted(t *threads.Thread, at map[anchor]diff.Line) []row {
	ln, ok := at[anchor{path: t.Path, side: t.Side, line: t.Line}]
	if !ok {
		return nil
	}

	return []row{{kind: rowCode, line: ln, path: t.Path, comment: noComment, hunk: ln.Hunk}}
}

// anchorWord names the line a thread answers, which the diff view does not have
// to say because the thread is drawn under it.
func anchorWord(t *threads.Thread) string {
	if t.Side == "LEFT" {
		return fmt.Sprintf("left %d", t.Line)
	}

	return fmt.Sprintf("line %d", t.Line)
}

// anchorLines is every commentable line of the diff by the anchor that reaches
// it, so a thread can be drawn with its code without the diff around it.
func anchorLines(d *diff.Diff) map[anchor]diff.Line {
	out := map[anchor]diff.Line{}

	for i := range d.Files {
		f := &d.Files[i]
		for _, l := range f.Lines {
			out[anchorOf(f.NewPath, l)] = l
		}
	}

	return out
}

// threadPaths is every file carrying a conversation, in the order a reader
// would look for them rather than the order the forge answered in.
func threadPaths(ts []threads.Thread) []string {
	seen := map[string]bool{}
	out := []string{}

	for i := range ts {
		if !seen[ts[i].Path] {
			seen[ts[i].Path] = true
			out = append(out, ts[i].Path)
		}
	}

	sort.Strings(out)

	return out
}

func countThreads(ts []threads.Thread, path string) int {
	n := 0

	for i := range ts {
		if ts[i].Path == path {
			n++
		}
	}

	return n
}
