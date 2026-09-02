package prepared

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/seen"
)

// cached names the directories holding one file per head commit. Everything in
// them is rebuilt from the pull request, so a file no staged review points at
// is a file nothing will read again.
var cached = []string{"diff", "threads", "score"}

// Root is the checkout or state directory the review is staged under, which is
// where its caches live.
func Root(r *Review) string { return filepath.Dir(filepath.Dir(r.Path)) }

// Discard removes a staged review and everything kept for it.
func Discard(r *Review) error { return DiscardAt(Root(r), r.Number) }

// DiscardAt removes the review staged for a pull request under root, its read
// marks, and whatever Sweep then finds unreferenced. Nothing being there is not
// a failure: a review that posted was removed as it posted.
func DiscardAt(root string, number int) error {
	for _, path := range []string{artifact.Path(root, number), seen.Path(root, number)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}

	_, err := Sweep(root)

	return err
}

// Sweep drops the diff, threads, and rating cached against a head commit no
// staged review is pinned to, and reports how many files it removed.
//
// A review that will not parse leaves the sweep alone: its head is unknown, so
// every cache under the root could be the one it needs to be repaired against.
func Sweep(root string) (int, error) {
	rows, err := List(root)
	if err != nil && !errors.Is(err, ErrNoDir) {
		return 0, err
	}

	keep := make(map[string]bool, len(rows))

	for i := range rows {
		if rows[i].Broken != "" {
			return 0, nil
		}

		keep[rows[i].HeadSHA] = true
	}

	removed := 0

	for _, kind := range cached {
		n, err := sweepDir(filepath.Join(root, artifact.Dir, kind), keep)
		if err != nil {
			return removed, err
		}

		removed += n
	}

	return removed, nil
}

func sweepDir(dir string, keep map[string]bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", dir, err)
	}

	removed := 0

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || keep[strings.TrimSuffix(name, filepath.Ext(name))] {
			continue
		}

		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("removing %s: %w", filepath.Join(dir, name), err)
		}

		removed++
	}

	return removed, nil
}
