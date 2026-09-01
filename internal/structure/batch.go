package structure

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// workers bounds the subprocesses. A pull request's hunks number in the tens to
// the low hundreds, and one process per hunk with no ceiling is how a review
// screen stalls a laptop rather than reading a diff.
const workers = 8

// ReadAll reads every hunk, answering in the order it was given them so a
// caller can zip the results back onto whatever it built the hunks from.
//
// One failure fails the batch: a structural answer for some hunks and a
// text-only answer for the rest is a filter that hides different things in
// different places, which is worse than not offering it.
func ReadAll(ctx context.Context, hs []Hunk) ([]Reading, error) {
	out := make([]Reading, len(hs))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)

	for i := range hs {
		g.Go(func() error {
			r, err := Read(ctx, hs[i])
			if err != nil {
				return err
			}

			out[i] = r

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		//nolint:wrapcheck // Read's error already names the fragment it failed on
		return nil, err
	}

	return out, nil
}
