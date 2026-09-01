// Package diff parses a unified diff into the line numbers a review comment
// anchors to.
//
// It reads only what an anchor needs: which file a hunk belongs to, and which
// pre-image and post-image line each of its lines carries. A file that carries
// no commentable line -- a rename, a binary payload, a mode change -- is kept
// with a note saying which, because a reader has to know the change happened
// even though nothing in it can be commented on.
package diff

import (
	"strconv"
	"strings"
)

// Kinds of diff line, spelled the way the patch spells them.
const (
	KindContext = ' '
	KindAdd     = '+'
	KindRemove  = '-'
)

// Line is one line of a hunk. Old is its number in the pre-image and New its
// number in the post-image; the one a line does not exist in is zero.
type Line struct {
	Text string
	Kind byte
	Old  int
	New  int
	// Hunk numbers the @@ block this line came from, counted across the whole
	// diff. GitHub refuses a multi-line comment whose ends are in two blocks.
	Hunk int
}

// File is one file's lines, in patch order.
type File struct {
	OldPath string
	NewPath string
	// Note says why a file carries no lines: renamed, binary, or mode changed.
	// It is empty for a file whose content the patch spells out.
	Note  string
	Lines []Line
}

// Diff is a parsed unified diff.
type Diff struct {
	Files []File
	// Headers holds each hunk's @@ line, indexed by Line.Hunk minus one, for a
	// caller rendering the diff rather than anchoring into it.
	Headers []string
}

// Repeated reports the post-image paths the diff carries more than once, in
// patch order.
//
// A cumulative pull request diff carries each file once. A second entry for
// the same path means the input is a per-commit patch series, where the line
// numbers belong to an intermediate commit rather than the head, so every
// anchor into it is quoted from the wrong place.
func (d *Diff) Repeated() []string {
	seen := make(map[string]int, len(d.Files))

	var out []string

	for i := range d.Files {
		path := d.Files[i].NewPath
		if path == "" {
			path = d.Files[i].OldPath
		}

		const firstRepeat = 2

		seen[path]++
		if seen[path] == firstRepeat {
			out = append(out, path)
		}
	}

	return out
}

// Parse reads a unified diff. A patch it cannot make sense of yields fewer
// files rather than an error, because a diff is evidence and a parser that
// refuses one leaves the caller with nothing to check against.
func Parse(patch []byte) *Diff {
	var (
		out     Diff
		current *File
		old     int
		newLine int
		hunk    int
	)

	for _, raw := range strings.Split(string(patch), "\n") {
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			out.Files = append(out.Files, gitHeader(raw[len("diff --git "):]))
			current = &out.Files[len(out.Files)-1]
			old, newLine = 0, 0
		case current == nil:
			continue
		// A file header only precedes the first hunk. Inside one, "--- " is a
		// removed line whose text starts with "-- ", which is every SQL comment.
		case old == 0 && newLine == 0 && fileHeader(current, raw):
		case strings.HasPrefix(raw, "@@"):
			start := hunkStart(raw)
			old, newLine = start.old, start.new
			hunk++
			out.Headers = append(out.Headers, raw)
		case old == 0 && newLine == 0:
			continue
		default:
			appendLine(current, raw, hunk, &old, &newLine)
		}
	}

	return &out
}

func appendLine(f *File, raw string, hunk int, old, newLine *int) {
	if raw == "" {
		return
	}

	kind, text := raw[0], raw[1:]

	switch kind {
	case KindContext:
		f.Lines = append(f.Lines, Line{Kind: KindContext, Old: *old, New: *newLine, Hunk: hunk, Text: text})
		*old++
		*newLine++
	case KindAdd:
		f.Lines = append(f.Lines, Line{Kind: KindAdd, New: *newLine, Hunk: hunk, Text: text})
		*newLine++
	case KindRemove:
		f.Lines = append(f.Lines, Line{Kind: KindRemove, Old: *old, Hunk: hunk, Text: text})
		*old++
	}
}

// fileHeader reads one line of a file's preamble into current and reports
// whether it was one, so the caller can leave everything else alone.
func fileHeader(current *File, raw string) bool {
	switch {
	case strings.HasPrefix(raw, "--- "):
		current.OldPath = headerPath(raw[len("--- "):])
	case strings.HasPrefix(raw, "+++ "):
		current.NewPath = headerPath(raw[len("+++ "):])
	case strings.HasPrefix(raw, "rename from "):
		current.Note = "renamed from " + headerPath(raw[len("rename from "):])
	case strings.HasPrefix(raw, "Binary files "):
		current.Note = "binary"
	case strings.HasPrefix(raw, "new mode "):
		current.Note = "mode " + raw[len("new mode "):]
	default:
		return false
	}

	return true
}

// gitHeader reads the two paths off a "diff --git a/x b/y" line, which is the
// only place a rename or a binary payload names the file it changed. A path
// carrying a space is left to the --- and +++ lines, which quote it properly.
func gitHeader(s string) File {
	fields := strings.Fields(s)

	const wantPaths = 2
	if len(fields) != wantPaths {
		return File{}
	}

	return File{OldPath: headerPath(fields[0]), NewPath: headerPath(fields[1])}
}

// headerPath strips the a/ or b/ prefix git writes and the trailing tab some
// diff generators append. A /dev/null side yields the empty string.
func headerPath(s string) string {
	if tab := strings.IndexByte(s, '\t'); tab >= 0 {
		s = s[:tab]
	}

	if s == "/dev/null" {
		return ""
	}

	if len(s) > 2 && (s[:2] == "a/" || s[:2] == "b/") {
		return s[2:]
	}

	return s
}

// starts is a hunk's first line number on each side.
type starts struct {
	old int
	new int
}

// hunkStart reads a hunk's first pre-image and post-image line number out of
// its @@ header. A header it cannot read yields zeroes, which skips the
// hunk's body rather than numbering it from the wrong place.
func hunkStart(header string) starts {
	fields := strings.Fields(header)

	const wantFields = 3
	if len(fields) < wantFields || !strings.HasPrefix(fields[1], "-") || !strings.HasPrefix(fields[2], "+") {
		return starts{}
	}

	return starts{old: rangeStart(fields[1][1:]), new: rangeStart(fields[2][1:])}
}

func rangeStart(s string) int {
	if comma := strings.IndexByte(s, ','); comma >= 0 {
		s = s[:comma]
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}

	return n
}
