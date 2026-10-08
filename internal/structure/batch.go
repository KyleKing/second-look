package structure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stagePerm is what a staged fragment is written with: it is code the diff
// already carries, but there is no call to leave it readable past the process.
const stagePerm = 0o600

// errStrayMatch is a match reported for a file nothing staged, which is the
// scan's contract broken rather than a fragment's problem.
var errStrayMatch = errors.New("the scan reported a file nothing staged")

// staged is a hunk that needs the grammar: its index, its language, and both
// sides as single sources.
type staged struct {
	hunk  int
	lang  Lang
	path  string
	sides [2]string
}

// sideName is what a fragment file is called between its index and its
// extension, which is how a match finds its way back to a hunk's before or
// after side.
var sideName = [2]string{"before", "after"}

// ReadAll reads every hunk, answering in the order it was given them so a
// caller can zip the results back onto whatever it built the hunks from.
//
// Every fragment that needs a grammar is staged as a file and scanned in one
// ast-grep invocation: the measured cost of the pass is the process, not the
// reading, so the batch pays it once rather than twice a hunk.
//
// One failure fails the batch: a structural answer for some hunks and a
// text-only answer for the rest is a filter that hides different things in
// different places, which is worse than not offering it.
func ReadAll(ctx context.Context, hs []Hunk) ([]Reading, error) {
	out := make([]Reading, len(hs))
	available := Available()

	var stages []staged

	for i := range hs {
		before, after := strings.Join(hs[i].Before, "\n"), strings.Join(hs[i].After, "\n")
		lang, ok := langFor(hs[i].Path)

		switch {
		case bare(before) == bare(after):
			out[i] = Reading{Change: ChangeLayout}
		case !ok || !available:
			out[i] = Reading{Change: ChangeCode}
		default:
			stages = append(stages, staged{
				hunk: i, lang: lang, path: hs[i].Path, sides: [2]string{before, after},
			})
		}
	}

	if len(stages) == 0 {
		return out, nil
	}

	was, now, err := scanStaged(ctx, stages)
	if err != nil {
		return nil, err
	}

	for i := range stages {
		s := &stages[i]
		out[s.hunk] = analyze(s.sides[0], s.sides[1], was[s.hunk], now[s.hunk])
	}

	return out, nil
}

// scanStaged writes every staged side to a directory and runs the one scan
// over all of them, answering the matches grouped by hunk and side.
func scanStaged(ctx context.Context, stages []staged) (map[int][]match, map[int][]match, error) {
	dir, err := os.MkdirTemp("", "second-look-structure-")
	if err != nil {
		return nil, nil, fmt.Errorf("staging the fragments: %w", err)
	}
	//nolint:errcheck // a leftover temp directory is not worth failing a read
	defer func() { _ = os.RemoveAll(dir) }()

	var ls []Lang
	inBatch := map[string]bool{}
	byFile := map[string][2]int{}

	for i := range stages {
		s := &stages[i]

		if !inBatch[s.lang.Name] {
			inBatch[s.lang.Name] = true
			ls = append(ls, s.lang)
		}

		ext := filepath.Ext(s.path)

		for side, src := range s.sides {
			if src == "" {
				continue
			}

			name := fmt.Sprintf("%d.%s%s", s.hunk, sideName[side], ext)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), stagePerm); err != nil {
				return nil, nil, fmt.Errorf("staging the fragment for %s: %w", s.path, err)
			}

			byFile[name] = [2]int{s.hunk, side}
		}
	}

	matches, err := scan(ctx, dir, ls)
	if err != nil {
		return nil, nil, err
	}

	before, after := map[int][]match{}, map[int][]match{}

	for _, m := range matches {
		at, ok := byFile[m.File]
		if !ok {
			return nil, nil, fmt.Errorf("%q: %w", m.File, errStrayMatch)
		}

		if at[1] == 0 {
			before[at[0]] = append(before[at[0]], m)
		} else {
			after[at[0]] = append(after[at[0]], m)
		}
	}

	return before, after, nil
}
