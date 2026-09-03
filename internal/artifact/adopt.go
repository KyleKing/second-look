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

// ErrTwoCopies reports the same file on both sides of a move. Choosing between
// them would drop staged work, so the migration stops and names both.
var ErrTwoCopies = errors.New("this file is staged in both places")

// ErrTwoRepos reports a directory staging reviews for more than one repository,
// which no single store answers for.
var ErrTwoRepos = errors.New("reviews here name more than one repository")

// Adopt moves an artifact tree left in a working copy into the store, so a pull
// request read from two clones is one review rather than two.
//
// It is safe to run on every open: a tree already moved is nothing to move, and
// a file that exists on both sides stops the whole migration rather than
// picking one.
func Adopt(from, to string) error {
	src, dst := filepath.Join(from, Dir), filepath.Join(to, Dir)

	if from == "" || to == "" || src == dst {
		return nil
	}

	moves, err := movable(src, dst)
	if err != nil || len(moves) == 0 {
		return err
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

// movable is every file under src, relative to it, refusing the whole move when
// one of them is already in the store.
func movable(src, dst string) ([]string, error) {
	var out []string

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
			return fmt.Errorf("%s and %s: %w", path, filepath.Join(dst, rel), ErrTwoCopies)
		}

		out = append(out, rel)

		return nil
	})

	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", src, err)
	}

	return out, nil
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
