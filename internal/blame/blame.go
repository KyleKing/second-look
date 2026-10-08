// Package blame ages the old side of a diff: which commit wrote each line a
// hunk carries, asked of the checkout's own history. The revision it is asked
// at is the caller's, since the answer is only honest at the base the diff
// was cut against.
package blame

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kyleking/aragonite/vcs"
	"golang.org/x/sync/errgroup"

	"github.com/kyleking/second-look/internal/artifact"
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

// Cached keeps a query's per-file answers under root, keyed by the blob the
// file's old side is: a push that lands elsewhere leaves the answer worth
// keeping, where a head key would throw it away. A file whose diff names no
// old blob asks fresh every time.
func Cached(root string, query Query, d *diff.Diff) Query {
	blobs := make(map[string]string, len(d.Files))
	for i := range d.Files {
		blobs[askPath(&d.Files[i])] = d.Files[i].OldBlob
	}

	return func(ctx context.Context, path string, rs []vcs.LineRange) ([]vcs.BlameLine, error) {
		key := cacheKey(blobs[path], path, rs)
		if key != "" {
			var lines []vcs.BlameLine
			if err := artifact.LoadBlame(root, key, &lines); err != nil {
				return nil, fmt.Errorf("reading the kept blame: %w", err)
			}

			if lines != nil {
				return lines, nil
			}
		}

		lines, err := query(ctx, path, rs)
		if err != nil {
			return nil, err
		}

		if key != "" {
			if err := artifact.SaveBlame(root, key, lines); err != nil {
				return nil, fmt.Errorf("keeping %s's blame: %w", path, err)
			}
		}

		return lines, nil
	}
}

// cacheKey hashes the one question blame answers for a file: this path's old
// blob, these ranges. An empty blob means the diff named no objects, and no
// key means the caller asks fresh.
func cacheKey(blob, path string, rs []vcs.LineRange) string {
	if blob == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(path)
	b.WriteByte(0)
	b.WriteString(blob)

	for _, r := range rs {
		b.WriteString(strconv.Itoa(r.From))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(r.To))
		b.WriteByte(';')
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:])
}

// askPath is the path the file's old side lives under: its own name, and the
// name it was renamed from when it has one.
func askPath(f *diff.File) string {
	if f.OldPath != "" {
		return f.OldPath
	}

	return f.NewPath
}

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
		path := askPath(f)

		show := f.NewPath
		if show == "" {
			show = f.OldPath
		}

		group.Go(func() error {
			lines, err := query(ctx, path, rs)
			if err != nil {
				return fmt.Errorf("blaming %s: %w", path, err)
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
