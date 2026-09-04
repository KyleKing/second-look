// Package brief renders a review for an agent to read.
//
// `show` prints the review and cannot hand over the code, so an agent asked
// about a finding is working from a path and a line number and has to go and
// find the diff itself. What it reads should be what the person is looking at,
// which means the diff with the anchors marked on it, and one comment with the
// hunk, the file around it, the conversation, and the private note together.
//
// The output is text rather than JSON because it is read rather than parsed:
// every other command that answers a machine already prints JSON.
package brief

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
)

// numbers is the width the line numbers are padded to, wide enough for a file
// nobody reviews a diff of.
const numbers = 5

// Diff prints the diff with each staged comment marked on the line it anchors
// to, so an agent reads the change and what is said about it as one thing.
func Diff(d *diff.Diff, r *artifact.Review) string {
	at := anchored(r)

	var b strings.Builder

	for i := range d.Files {
		f := &d.Files[i]

		fmt.Fprintf(&b, "%s\n", pathOf(f))

		if f.Note != "" {
			fmt.Fprintf(&b, "  %s\n\n", f.Note)

			continue
		}

		hunk := 0

		for _, ln := range f.Lines {
			if ln.Hunk != hunk {
				hunk = ln.Hunk
				fmt.Fprintf(&b, "  %s\n", header(d, hunk))
			}

			b.WriteString("  " + line(ln))

			for _, c := range at[anchorOf(pathOf(f), ln)] {
				fmt.Fprintf(&b, "    <<< %s\n", mark(c))
			}
		}

		b.WriteString("\n")
	}

	return b.String()
}

// Comment is everything about one staged comment: where it sits, what it says,
// the note that never posts, the hunk it anchors in, the file around it, and
// the conversation it answers.
func Comment(
	r *artifact.Review, id string, d *diff.Diff, ts []threads.Thread, around int,
) (string, error) {
	c := r.Find(id)
	if c == nil {
		return "", fmt.Errorf("%q: %w", id, ErrNoComment)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "%s  %s\n", mark(c), where(c))
	fmt.Fprintf(&b, "\n%s\n", indent(c.Body))

	if strings.TrimSpace(c.Note) != "" {
		fmt.Fprintf(&b, "\nNOTE (never posted)\n%s\n", indent(c.Note))
	}

	if c.SkipReason != "" {
		fmt.Fprintf(&b, "\nSKIPPED: %s\n", c.SkipReason)
	}

	b.WriteString(Turns(c))
	b.WriteString(hunkOf(d, c, around))
	b.WriteString(conversation(c, ts))

	return b.String(), nil
}

// hunkOf is the hunk the comment anchors in, with the anchor line marked and
// the rest of the hunk around it. A hunk is the context the diff carries, and
// asking for more of the file than it holds is step 10's business.
func hunkOf(d *diff.Diff, c *artifact.Comment, around int) string {
	hunk, ok := d.HunkOf(c.Path, c.Side, c.Line)
	if !ok {
		return "\nThe diff does not carry this line; the head has moved under it.\n"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "\n%s\n  %s\n", c.Path, header(d, hunk))

	for i := range d.Files {
		if pathOf(&d.Files[i]) != c.Path {
			continue
		}

		for _, ln := range d.Files[i].Lines {
			if ln.Hunk != hunk || !within(c, ln, around) {
				continue
			}

			if marks(c, ln) {
				b.WriteString(">>" + line(ln))

				continue
			}

			b.WriteString("  " + line(ln))
		}
	}

	return b.String()
}

// within keeps a line the comment covers, and as many either side as asked
// for. A zero window is the whole hunk, which is what an agent reading one
// finding usually wants.
func within(c *artifact.Comment, ln diff.Line, around int) bool {
	if around <= 0 {
		return true
	}

	n := ln.New
	if c.Side == artifact.SideLeft {
		n = ln.Old
	}

	from := c.Line
	if c.StartLine != 0 {
		from = c.StartLine
	}

	return n >= from-around && n <= c.Line+around
}

func marks(c *artifact.Comment, ln diff.Line) bool {
	n := ln.New
	if c.Side == artifact.SideLeft {
		n = ln.Old
	}

	from := c.Line
	if c.StartLine != 0 {
		from = c.StartLine
	}

	return n != 0 && n >= from && n <= c.Line
}

func conversation(c *artifact.Comment, ts []threads.Thread) string {
	if c.InReplyTo == 0 {
		return ""
	}

	for i := range ts {
		if ts[i].ReplyTo() != c.InReplyTo {
			continue
		}

		var b strings.Builder

		b.WriteString("\nIT ANSWERS\n")

		for _, n := range ts[i].Notes {
			fmt.Fprintf(&b, "  @%s\n%s\n", n.Author, indent("  "+n.Body))
		}

		return b.String()
	}

	return fmt.Sprintf("\nIT ANSWERS comment %d, which is not in the cached threads.\n", c.InReplyTo)
}

// anchored indexes the comments by the line they hang from, since a line can
// carry several and the diff is walked once.
func anchored(r *artifact.Review) map[anchor][]*artifact.Comment {
	out := map[anchor][]*artifact.Comment{}

	for i := range r.Comments {
		c := &r.Comments[i]
		if c.InReplyTo != 0 && c.Path == "" {
			continue
		}

		out[anchor{c.Path, c.Side, c.Line}] = append(out[anchor{c.Path, c.Side, c.Line}], c)
	}

	return out
}

type anchor struct {
	path string
	side string
	line int
}

func anchorOf(path string, ln diff.Line) anchor {
	if ln.Kind == diff.KindRemove {
		return anchor{path, artifact.SideLeft, ln.Old}
	}

	return anchor{path, artifact.SideRight, ln.New}
}

func mark(c *artifact.Comment) string {
	out := c.ID + "  " + c.Status
	if c.Severity != "" {
		out = c.ID + "  " + c.Severity + "/" + c.Status
	}

	return out
}

func where(c *artifact.Comment) string {
	if c.StartLine != 0 {
		return fmt.Sprintf("%s %s %d-%d", c.Path, c.Side, c.StartLine, c.Line)
	}

	return fmt.Sprintf("%s %s %d", c.Path, c.Side, c.Line)
}

func line(ln diff.Line) string {
	return fmt.Sprintf("%*s %c %s\n", numbers, number(ln), ln.Kind, ln.Text)
}

func number(ln diff.Line) string {
	if ln.Kind == diff.KindRemove {
		return strconv.Itoa(ln.Old)
	}

	return strconv.Itoa(ln.New)
}

func header(d *diff.Diff, hunk int) string {
	if hunk < 1 || hunk > len(d.Headers) {
		return "@@"
	}

	return d.Headers[hunk-1]
}

func pathOf(f *diff.File) string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}

func indent(s string) string {
	var b strings.Builder

	for l := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("  " + l + "\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
