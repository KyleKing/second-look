package get

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/stash"
)

// Confirm asks a yes-or-no question. It answers false for a run nobody is
// watching, so an agent or a pipe never has its working tree moved.
type Confirm func(question string) (bool, error)

// Prepare moves the checkout onto a pull request's head so its review can be
// opened, and parks uncommitted work first when ask agrees to it.
//
// Turning the stash down leaves the tree untouched and the move undone, which is
// the other way out: commit or stash by hand, then ask again.
func Prepare(ctx context.Context, out io.Writer, root string, number int, ask Confirm) error {
	err := Run(ctx, out, root, number)
	if err == nil || !errors.Is(err, ErrDirtyTree) {
		return err
	}

	yes, askErr := ask(fmt.Sprintf("%s. Stash them and check out #%d?", dirty(ctx, root), number))
	if askErr != nil {
		return askErr
	}

	// The tree is as it was, so the reason it blocked still stands.
	if !yes {
		return err
	}

	if err := stash.Push(ctx, root, fmt.Sprintf("second-look: before reviewing #%d", number)); err != nil {
		//nolint:wrapcheck // Push's own error already names what failed
		return err
	}

	if err := say(out, "stashed your changes; "+stash.Restore+" brings them back\n"); err != nil {
		return err
	}

	return Run(ctx, out, root, number)
}

// dirty describes what the move would clobber, so the question names a number of
// files rather than asking about "changes" in the abstract.
func dirty(ctx context.Context, root string) string {
	summary, err := vcs.GetOperations(root).GetRepoSummary(ctx, root)
	if err != nil {
		return "The working tree has uncommitted changes"
	}

	n := summary.UncommittedCount()
	if n == 1 {
		return "1 file has uncommitted changes"
	}

	return fmt.Sprintf("%d files have uncommitted changes", n)
}
