package prepared

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/humanize"
)

// Write prints the staged reviews for a person to read. An empty list still
// prints a line saying so, because "nothing is staged here" is a real answer
// and an empty screen is not.
func Write(w io.Writer, rows []Review, now time.Time) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "nothing staged under .second-look")

		return wrap(err)
	}

	width := 0
	for i := range rows {
		width = max(width, humanize.Width(rows[i].Where()))
	}

	for i := range rows {
		if _, err := fmt.Fprintln(w, line(&rows[i], now, width)); err != nil {
			return wrap(err)
		}
	}

	return nil
}

// line is one staged review, in the order a reader scans: which pull request,
// how stale, what state it is in, then what it holds.
func line(r *Review, now time.Time, whereWidth int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%-*s  %5s  %s", whereWidth, r.Where(), humanize.Ago(r.Modified, now), state(r))

	if r.Broken != "" {
		b.WriteString("  " + r.Broken)

		return b.String()
	}

	b.WriteString("  " + counts(r))

	if r.HeadSHA != "" {
		b.WriteString("  @" + r.Short())
	}

	return b.String()
}

// state is the one word that says what to do with the review: a draft blocks
// the submit, and everything else is ready to post.
func state(r *Review) string {
	switch {
	case r.Broken != "":
		return "unreadable"
	case r.Blocked():
		return "blocked   "
	case r.Ready > 0 || r.Body:
		return "ready     "
	default:
		return "empty     "
	}
}

// counts spells out what the review holds. A zero is left out rather than
// printed, so the row carries only what is there.
func counts(r *Review) string {
	parts := make([]string, 0, 5)

	for _, c := range []struct {
		n      int
		name   string
		plural string
	}{
		{r.Ready, "ready", "ready"},
		{r.Draft, "draft", "drafts"},
		{r.Skip, "skipped", "skipped"},
		{r.Replies, "reply", "replies"},
	} {
		if c.n == 0 {
			continue
		}

		name := c.name
		if c.n > 1 {
			name = c.plural
		}

		parts = append(parts, strconv.Itoa(c.n)+" "+name)
	}

	if r.Body {
		parts = append(parts, "body")
	}

	if r.Event != "" {
		parts = append(parts, strings.ToLower(r.Event))
	}

	if len(parts) == 0 {
		return "no comments"
	}

	return strings.Join(parts, " · ")
}

func wrap(err error) error {
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}
