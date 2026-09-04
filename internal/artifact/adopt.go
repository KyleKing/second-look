package artifact

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Pointer is the file left in a checkout naming the store its reviews moved to.
// It is prose rather than a format: nothing reads it back, since the store is
// derived from the repository, and what it is for is a person or an agent who
// looks in the checkout and finds the directory nearly empty.
const Pointer = "WHERE.md"

// ErrTwoRepos reports a directory staging reviews for more than one repository,
// which no single store answers for.
var ErrTwoRepos = errors.New("reviews here name more than one repository")

// Adopt moves an artifact tree left in a working copy into the store, so a pull
// request read from two clones is one review rather than two.
//
// It is safe to run on every open: a tree already moved is nothing to move, and
// a file the store already holds stays where it is rather than overwriting a
// copy that may carry different staged work. What stays keeps the directory
// live, so `reviews` lists it as a stray for a person to settle by hand.
func Adopt(from, to string) error {
	src, dst := filepath.Join(from, Dir), filepath.Join(to, Dir)

	if from == "" || to == "" || src == dst {
		return nil
	}

	moves, kept, err := movable(src, dst)
	if err != nil {
		return err
	}

	if len(moves) == 0 && len(kept) == 0 {
		return nil
	}

	if err := ensureDir(dst); err != nil {
		return err
	}

	for _, rel := range moves {
		if err := move(filepath.Join(src, rel), filepath.Join(dst, rel)); err != nil {
			return err
		}
	}

	prune(src)

	// A directory still holding staged work must not say nothing here is read.
	if len(kept) > 0 {
		return nil
	}

	return pointAt(src, to)
}

// AdoptHere moves the tree in a directory into whichever store its reviews
// name, which is what a command holding no target of its own can still do. A
// tree carrying no readable review names no repository and is left alone.
func AdoptHere(root string) error {
	entries, err := os.ReadDir(filepath.Join(root, Dir))
	if err != nil {
		return nil //nolint:nilerr // no tree here is nothing to move, not a failure
	}

	var to string

	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "pr-") {
			continue
		}

		r, err := Load(filepath.Join(root, Dir, e.Name()))
		if err != nil {
			continue
		}

		store, err := StateRoot(hostOf(r), r.Owner, r.Repo)
		if err != nil {
			return err
		}

		if to != "" && to != store {
			return fmt.Errorf("%s: %w", filepath.Join(root, Dir), ErrTwoRepos)
		}

		to = store
	}

	if to == "" {
		return nil
	}

	return Adopt(root, to)
}

func hostOf(r *Review) string {
	if r.Host == "" {
		return "github.com"
	}

	return r.Host
}

// movable splits the files under src, relative to it, into the ones to move and
// the ones the store already holds. One of the second kind used to stop the
// whole migration, which hid every other review behind a single duplicate.
func movable(src, dst string) ([]string, []string, error) {
	var out, kept []string

	err := filepath.WalkDir(src, func(path string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case e.IsDir():
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("naming %s under %s: %w", path, src, err)
		}

		// The ignore file and the pointer are second-look's own furniture: the
		// store writes its own and neither carries staged work.
		if rel == ".gitignore" || rel == Pointer {
			return nil
		}

		if _, err := os.Stat(filepath.Join(dst, rel)); err == nil {
			kept = append(kept, rel)

			return nil
		}

		out = append(out, rel)

		return nil
	})

	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}

	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", src, err)
	}

	return out, kept, nil
}

// move renames a file, copying it when the two sit on different filesystems.
func move(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(to), err)
	}

	if err := os.Rename(from, to); err == nil {
		return nil
	}

	if err := copyFile(from, to); err != nil {
		return err
	}

	if err := os.Remove(from); err != nil {
		return fmt.Errorf("removing %s: %w", from, err)
	}

	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from) //nolint:gosec // the path is one this package wrote
	if err != nil {
		return fmt.Errorf("reading %s: %w", from, err)
	}
	defer in.Close() //nolint:errcheck // read-only

	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm) //nolint:gosec // a path this package wrote
	if err != nil {
		return fmt.Errorf("creating %s: %w", to, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck,gosec // the copy already failed

		return fmt.Errorf("writing %s: %w", to, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", to, err)
	}

	return nil
}

// prune drops the directories the move emptied, deepest first, and leaves
// anything still holding a file alone.
func prune(src string) {
	var dirs []string

	//nolint:errcheck,gosec // a directory that cannot be walked is one to leave alone
	filepath.WalkDir(src, func(path string, e fs.DirEntry, err error) error {
		if err == nil && e.IsDir() && path != src {
			dirs = append(dirs, path)
		}

		return nil
	})

	for i := len(dirs) - 1; i >= 0; i-- {
		os.Remove(dirs[i]) //nolint:errcheck,gosec // a directory still holding something stays
	}
}

func pointAt(src, to string) error {
	text := fmt.Sprintf(`# Moved

second-look keeps every review, read mark, and cached diff for this repository
in one place, so reading a pull request from two clones is one review:

    %s

Nothing here is read any more. `+"`second-look reviews`"+` lists what is staged.
`, filepath.Join(to, Dir))

	if err := os.WriteFile(filepath.Join(src, Pointer), []byte(text), filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Join(src, Pointer), err)
	}

	return nil
}
