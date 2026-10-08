// Package blame ages the old side of a diff: which commit wrote each line a
// hunk carries, asked of the checkout's own history. The revision it is asked
// at is the caller's, since the answer is only honest at the base the diff
// was cut against.
package blame

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kyleking/aragonite/vcs"
	"golang.org/x/sync/errgroup"

	"github.com/kyleking/second-look/internal/diff"
)

// Line is one old-side line's attribution: the commit that last touched it,
// when, and by whom.
type Line struct {
	When   time.Time
	Commit string
	Author string
}

// Map is a whole diff's attribution, keyed by the path the diff names the
// file under and the line's number on the old side. A renamed file answers
// under its new name, which is the name every row is drawn with, though the
// history itself was asked for under the old one.
type Map map[string]map[int]Line

// Query blames ranges of one file. The revision and the repository are the
// caller's to close over, which keeps both where the checkout and the merge
// base are known.
type Query func(ctx context.Context, path string, ranges []vcs.LineRange) ([]vcs.BlameLine, error)

// blameAll is the most files one pass asks about at once, which is the bound
// the parallel blame calls share.
const blameAll = 8

// Read blames every file's old side, one call per file. A file with no
// old-side lines, an added file, asks nothing, and a failed query fails the
// pass, since a gutter half-drawn reads as history answered rather than
// refused.
func Read(ctx context.Context, query Query, d *diff.Diff) (Map, error) {
	var (
		group errgroup.Group
		mu    sync.Mutex
		out   = Map{}
	)

	group.SetLimit(blameAll)

	for i := range d.Files {
		f := &d.Files[i]

		rs := ranges(f)
		if len(rs) == 0 {
			continue
		}

		// A rename's old side lives at the old path, while its rows are drawn
		// under the new one.
		ask := f.OldPath
		if ask == "" {
			ask = f.NewPath
		}

		show := f.NewPath
		if show == "" {
			show = f.OldPath
		}

		group.Go(func() error {
			lines, err := query(ctx, ask, rs)
			if err != nil {
				return fmt.Errorf("blaming %s: %w", ask, err)
			}

			found := make(map[int]Line, len(lines))
			for _, l := range lines {
				found[l.Line] = Line{When: l.When, Commit: l.Commit, Author: l.Author}
			}

			mu.Lock()
			out[show] = found
			mu.Unlock()

			return nil
		})
	}

	//nolint:wrapcheck // Wait returns this pass's own errors, which already name the file
	if err := group.Wait(); err != nil {
		return nil, err
	}

	return out, nil
}

// ranges is the old-side span of each of a file's hunks, one per hunk. A hunk
// that adds without removing still carries context lines, which are old-side
// lines too.
func ranges(f *diff.File) []vcs.LineRange {
	var out []vcs.LineRange

	last := -1

	for _, l := range f.Lines {
		if l.Old == 0 {
			continue
		}

		if l.Hunk != last {
			out = append(out, vcs.LineRange{From: l.Old, To: l.Old})
			last = l.Hunk

			continue
		}

		out[len(out)-1].To = l.Old
	}

	return out
}
