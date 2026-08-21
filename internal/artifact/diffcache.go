package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiffPath is where the diff for a head commit is cached. Keying by commit
// rather than by pull request means a review of an older head keeps the diff
// its anchors were quoted from.
func DiffPath(root, sha string) string {
	return filepath.Join(root, Dir, "diff", sha+".patch")
}

// SaveDiff caches a pull request's diff. It refuses a sha that is not a plain
// hex object name, since the value reaches the filesystem as a path.
func SaveDiff(root, sha string, patch []byte) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	path := DiffPath(root, sha)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	if err := os.WriteFile(path, patch, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// LoadDiff reads the cached diff for a head commit.
func LoadDiff(root, sha string) ([]byte, error) {
	if err := checkSHA(sha); err != nil {
		return nil, err
	}

	path := DiffPath(root, sha)

	patch, err := os.ReadFile(path) //nolint:gosec // the path is the repo root plus a checked hex sha
	if err != nil {
		return nil, fmt.Errorf("reading the cached diff: %w", err)
	}

	return patch, nil
}

func checkSHA(sha string) error {
	if sha == "" {
		return ErrNoHeadSHA
	}

	if strings.TrimLeft(sha, "0123456789abcdefABCDEF") != "" {
		return fmt.Errorf("%w: %q", ErrNotASHA, sha)
	}

	return nil
}
