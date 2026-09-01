package inbox

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/humanize"
)

// Write prints the queue for a person to read. Buckets keep their order and an
// empty one still prints its heading, because "nothing is waiting on you" is
// the answer most worth seeing.
func Write(w io.Writer, buckets []Bucket, now time.Time) error {
	for i, b := range buckets {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return fmt.Errorf("writing output: %w", err)
			}
		}

		if err := writeBucket(w, b, now); err != nil {
			return err
		}
	}

	return nil
}

func writeBucket(w io.Writer, b Bucket, now time.Time) error {
	if _, err := fmt.Fprintf(w, "%s (%d)\n", b.Name, len(b.Items)); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	if b.Err != "" {
		_, err := fmt.Fprintf(w, "  could not be read: %s\n", firstSentence(b.Err))

		return wrapWrite(err)
	}

	w1, w2 := widths(b.Items)

	for i := range b.Items {
		if _, err := fmt.Fprintln(w, "  "+line(&b.Items[i], now, w1, w2)); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	}

	return nil
}

// widths measures the two columns that vary, so a bucket's lines align with
// each other. Aligning across buckets would make the widest repository name
// anywhere set the indent everywhere, which is worse than a seam between them.
func widths(items []PullRequest) (int, int) {
	const authorCap = 14

	var name, author int

	for i := range items {
		name = max(name, humanize.Width(where(&items[i])))
		author = max(author, min(authorCap, humanize.Width(items[i].Author)))
	}

	return name, author
}

func where(p *PullRequest) string { return fmt.Sprintf("%s#%d", p.Repository, p.Number) }

// firstSentence is as much of a failure as a queue is worth spending. GitHub
// answers a rate limit with four hundred characters of terms of service and a
// request id; the reason is the first sentence and --json keeps the rest.
func firstSentence(s string) string {
	if at := strings.Index(s, ". "); at > 0 {
		return s[:at+1] + " (--json has the rest)"
	}

	return s
}

func wrapWrite(err error) error {
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

// line is one pull request, in the order a reader scans: where it is, who wrote
// it, how stale it is, then the title, which is the only part that runs long.
func line(p *PullRequest, now time.Time, nameWidth, authorWidth int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%-*s  %-*s  %5s",
		nameWidth, where(p), authorWidth, humanize.Clip(p.Author, authorWidth), humanize.Ago(p.Updated, now))

	if p.Draft {
		b.WriteString("  draft")
	}

	if len(p.Labels) > 0 {
		b.WriteString("  [" + strings.Join(p.Labels, " ") + "]")
	}

	b.WriteString("  " + p.Title)

	return b.String()
}
