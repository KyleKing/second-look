package conversations

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/humanize"
)

// Write prints the queue for a person to read. Buckets keep their order and an
// empty one still prints its heading, because "nothing moved since you looked"
// is the answer most worth seeing.
func Write(w io.Writer, buckets []Bucket, now time.Time) error {
	for i := range buckets {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return wrap(err)
			}
		}

		if err := writeBucket(w, &buckets[i], now); err != nil {
			return err
		}
	}

	return nil
}

func writeBucket(w io.Writer, b *Bucket, now time.Time) error {
	if _, err := fmt.Fprintf(w, "%s (%d)\n", b.Name, len(b.Items)); err != nil {
		return wrap(err)
	}

	where, anchor := widths(b.Items)

	for i := range b.Items {
		c := &b.Items[i]

		if _, err := fmt.Fprintln(w, "  "+line(c, now, where, anchor)); err != nil {
			return wrap(err)
		}

		if _, err := fmt.Fprintln(w, "      "+said(c)); err != nil {
			return wrap(err)
		}
	}

	return nil
}

// widths measures the two columns that vary, so a bucket's lines align with
// each other. Aligning across buckets would make the longest path anywhere set
// the indent everywhere, which is worse than a seam between them.
func widths(items []Conversation) (int, int) {
	const anchorCap = 44

	var where, anchor int

	for i := range items {
		where = max(where, humanize.Width(items[i].Where()))
		anchor = max(anchor, min(anchorCap, humanize.Width(items[i].Anchor())))
	}

	return where, anchor
}

// line is one conversation, in the order a reader scans: which pull request,
// where in it, whose turn, how stale, then the title.
func line(c *Conversation, now time.Time, whereWidth, anchorWidth int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%-*s  %-*s  %5s  %s",
		whereWidth, c.Where(),
		anchorWidth, humanize.Clip(c.Anchor(), anchorWidth),
		humanize.Ago(c.Updated(), now),
		turn(c))

	if n := len(c.Notes); n > 1 {
		b.WriteString("  " + strconv.Itoa(n) + " replies")
	}

	if c.Outdated {
		b.WriteString("  outdated")
	}

	b.WriteString("  " + c.Title)

	return b.String()
}

// turn is who spoke last, which is the field that says whether the next word is
// yours. It is the author rather than a yes-or-no, because a thread a bot last
// touched is a different obligation from one a person last touched.
func turn(c *Conversation) string {
	const authorCap = 16

	return humanize.Clip(c.Last().Author, authorCap)
}

// said is the opening line of the last comment. It is what makes a row worth
// reading without opening the pull request, which is the whole reason the queue
// exists.
func said(c *Conversation) string {
	const bodyCap = 96

	body := humanize.FirstLine(c.Last().Body)
	if body == "" {
		return "(no text)"
	}

	return humanize.Clip(body, bodyCap)
}

func wrap(err error) error {
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
