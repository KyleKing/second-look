package artifact

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
)

// Resolve quotes the diff line each comment anchors to. A comment whose line
// the diff does not carry is reported rather than stored, because GitHub
// refuses a comment outside the diff and a line number invented out of
// nothing lands there.
//
// A reply carries no anchor of its own: it lands under the comment it answers.
func Resolve(comments []Comment, d *diff.Diff) error {
	if err := usable(d); err != nil {
		return err
	}

	var errs []error

	for i := range comments {
		c := &comments[i]
		if c.InReplyTo != 0 {
			continue
		}

		text, ok := d.Anchor(c.Path, c.Side, c.Line)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: %w: %s %s line %d",
				name(c, i), ErrAnchorMissing, c.Path, c.Side, c.Line))

			continue
		}

		if err := checkSpan(c, d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name(c, i), err))

			continue
		}

		c.Anchor = text
	}

	return errors.Join(errs...)
}

// Verify compares each comment's recorded anchor against the live diff byte
// for byte. Skipped comments and replies never post, so neither is checked.
func Verify(comments []Comment, d *diff.Diff) error {
	if err := usable(d); err != nil {
		return err
	}

	var errs []error

	for i := range comments {
		c := &comments[i]
		if c.InReplyTo != 0 || c.Status == StatusSkip {
			continue
		}

		if c.Anchor == "" {
			errs = append(errs, fmt.Errorf("%s: %w", name(c, i), ErrNoAnchor))

			continue
		}

		text, ok := d.Anchor(c.Path, c.Side, c.Line)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: %w: %s %s line %d",
				name(c, i), ErrAnchorMissing, c.Path, c.Side, c.Line))

			continue
		}

		if text != c.Anchor {
			errs = append(errs, fmt.Errorf("%s: %w\n  staged against: %s\n  now reads:      %s",
				name(c, i), ErrAnchorMoved, c.Anchor, text))

			continue
		}

		if err := checkSpan(c, d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name(c, i), err))
		}
	}

	return errors.Join(errs...)
}

// name identifies a comment in a message, since one with no id still has to be
// findable in the file.
func name(c *Comment, i int) string {
	if c.ID != "" {
		return c.ID
	}

	return fmt.Sprintf("comment %d", i)
}

// usable refuses a diff that carries a file twice. Every line number in such a
// patch belongs to some intermediate commit, so quoting one anchor from it is
// no safer than quoting all of them.
func usable(d *diff.Diff) error {
	repeated := d.Repeated()
	if len(repeated) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrNotACumulativeDiff, strings.Join(repeated, ", "))
}

// checkSpan holds a multi-line comment to what GitHub accepts: both ends in the
// diff, and both in the same hunk. Only the end line carries an anchor, so
// without this the start line is never looked at until the post is refused.
func checkSpan(c *Comment, d *diff.Diff) error {
	if c.StartLine == 0 {
		return nil
	}

	side := c.StartSide
	if side == "" {
		side = c.Side
	}

	start, ok := d.HunkOf(c.Path, side, c.StartLine)
	if !ok {
		return fmt.Errorf("%w: %s %s start_line %d", ErrAnchorMissing, c.Path, side, c.StartLine)
	}

	end, ok := d.HunkOf(c.Path, c.Side, c.Line)
	if !ok || start != end {
		return fmt.Errorf("%w: %s %d to %d", ErrSpansHunks, c.Path, c.StartLine, c.Line)
	}

	return nil
}
