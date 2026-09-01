package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ThreadsPath is where the pull request's open review threads are cached, keyed
// by head commit for the same reason the diff is: a thread anchors to a line
// number, and a line number belongs to one commit.
func ThreadsPath(root, sha string) string {
	return filepath.Join(root, Dir, "threads", sha+".json")
}

// SaveThreads caches whatever was read off the pull request. The value is
// context rather than state: it is rebuilt on every get and nothing reads it
// after the review posts.
func SaveThreads(root, sha string, v any) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding the review threads: %w", err)
	}

	path := ThreadsPath(root, sha)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// LoadThreads reads the cached threads into v. A missing cache is not an error:
// a review prepared before threads were fetched has none, and the screen shows
// the diff either way.
func LoadThreads(root, sha string, v any) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	body, err := os.ReadFile(ThreadsPath(root, sha))
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("reading the cached review threads: %w", err)
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("reading the cached review threads: %w", err)
	}

	return nil
}
