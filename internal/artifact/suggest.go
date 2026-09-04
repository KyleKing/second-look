package artifact

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
)

// Fence opens a GitHub suggestion. Everything between it and the closing fence
// replaces the lines the comment covers when the author presses commit.
const Fence = "```suggestion"

// Suggest wraps replacement text as a suggestion block. The text is taken as it
// is: a suggestion is code, and trimming it would change what lands.
func Suggest(text string) string {
	return Fence + "\n" + strings.TrimRight(text, "\n") + "\n```"
}

// Suggestion is the replacement a comment carries, and false for a comment that
// carries none.
func Suggestion(body string) (string, bool) {
	_, rest, ok := strings.Cut(body, Fence+"\n")
	if !ok {
		return "", false
	}

	text, _, ok := strings.Cut(rest, "\n```")
	if !ok {
		return "", false
	}

	return text, true
}

// CheckSuggestion holds a suggestion to what GitHub will accept, at staging
// time rather than at post time.
//
// A suggestion replaces lines of the file that results, so it can only hang
// from the right side, and every line it covers has to be a line that exists
// there. A range that crosses a removed line is the case that reads as fine and
// is refused on posting: the numbers are contiguous in the post-image and the
// diff shows a gap between them.
func CheckSuggestion(c *Comment, d *diff.Diff) error {
	if _, ok := Suggestion(c.Body); !ok {
		return nil
	}

	if c.Side != SideRight || (c.StartSide != "" && c.StartSide != SideRight) {
		return fmt.Errorf("%s: %w", name(c, 0), ErrSuggestionSide)
	}

	from := c.StartLine
	if from == 0 {
		from = c.Line
	}

	for line := from; line <= c.Line; line++ {
		if _, ok := d.Anchor(c.Path, SideRight, line); !ok {
			return fmt.Errorf("%s: %w: %s line %d", name(c, 0), ErrSuggestionGap, c.Path, line)
		}
	}

	return nil
}

// Drifted is every comment whose anchor no longer reads the way it did when it
// was staged, by id.
//
// It is the posting guard asked at read time. Finding out at submit that four
// comments moved under a force-push is finding out too late, and the same
// question costs nothing against a diff already in memory.
func Drifted(comments []Comment, d *diff.Diff) map[string]bool {
	out := map[string]bool{}

	for i := range comments {
		c := &comments[i]
		if c.InReplyTo != 0 || c.Status == StatusSkip || c.Anchor == "" {
			continue
		}

		text, ok := d.Anchor(c.Path, c.Side, c.Line)
		if !ok || text != c.Anchor {
			out[c.ID] = true
		}
	}

	return out
}
