package get

import (
	"context"
	"io"

	"github.com/kyleking/aragonite/forge"
)

// MoveWorkingCopy exposes checkout to black-box tests: a diverged upstream is
// real git state a test has to build against an actual checkout, not
// something a public constructor stands in for.
func MoveWorkingCopy(ctx context.Context, out io.Writer, t Target, pr *forge.PullRequest) error {
	return checkout(ctx, out, t, pr)
}
