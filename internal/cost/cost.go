// Package cost rates a pull request nobody has opened yet, which is what lets a
// queue be ordered by how much reading each row needs rather than by age.
//
// It is the same rating the review screen shows, made from the same pass. What
// differs is where the diff comes from: the screen has one cached against the
// head it was staged at, and a queue row has nothing on this laptop at all.
package cost

import (
	"context"
	"fmt"

	"github.com/kyleking/aragonite/forge/github"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/rate"
)

// Of fetches a pull request's diff and reads it. It is one API read per call,
// so a caller rating a queue bounds how many run at once.
func Of(ctx context.Context, dir, repository string, number int) (rate.Score, rate.Size, error) {
	patch, err := github.PRDiff(ctx, dir, repository, number)
	if err != nil {
		return rate.Score{}, rate.Size{}, fmt.Errorf("reading the diff of %s#%d: %w", repository, number, err)
	}

	d := diff.Parse(patch)

	readings, refs, err := rate.Read(ctx, d)
	if err != nil {
		return rate.Score{}, rate.Size{}, fmt.Errorf("rating %s#%d: %w", repository, number, err)
	}

	return rate.Of(readings), rate.Measure(d, readings, refs), nil
}
