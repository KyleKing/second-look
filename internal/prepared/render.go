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

	const stateWidth = 10

	fmt.Fprintf(&b, "%-*s  %5s  %-*s", whereWidth, r.Where(), humanize.Ago(r.Modified, now), stateWidth, State(r))

	if r.Broken != "" {
		b.WriteString("  " + r.Broken)

		return b.String()
	}

	b.WriteString("  " + Holds(r))

	return b.String()
}

// Holds spells out what the review carries. A zero is left out rather than
// printed, so the row carries only what is there.
//
// The text screen and the list screen both draw this line, so it lives here
// rather than in either: two copies drift, and the first thing they disagreed
// about was whether one reply is "1 reply" or "1 replies".
func Holds(r *Review) string {
	parts := make([]string, 0, 5)

	for _, c := range []struct {
		n      int
		name   string
		plural string
	}{
		{r.Ready, StateReady, StateReady},
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

	if r.HeadSHA != "" {
		parts = append(parts, "@"+r.Short())
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
