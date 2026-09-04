package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// What is cached against a head, one directory each. Every kind here is
// context rather than state: it is rebuilt on every get, nothing reads it after
// the review posts, and it is swept once no staged review is pinned to the head
// it was read at.
const (
	threadKind = "threads"
	aboutKind  = "about"
)

// ThreadsPath is where the pull request's open review threads are cached, keyed
// by head commit because a thread anchors to a line number and a line number
// belongs to one commit.
func ThreadsPath(root, sha string) string { return cachePath(root, threadKind, sha) }

// AboutPath is where the pull request's own context is cached: what the change
// is called, who wrote it, its description, and the comments left on the pull
// request rather than on a line of it.
func AboutPath(root, sha string) string { return cachePath(root, aboutKind, sha) }

// SaveThreads caches whatever was read off the pull request.
func SaveThreads(root, sha string, v any) error {
	return saveCached(root, threadKind, sha, "review threads", v)
}

// LoadThreads reads the cached threads into v. A missing cache is not an error:
// a review prepared before threads were fetched has none, and the screen shows
// the diff either way.
func LoadThreads(root, sha string, v any) error {
	return loadCached(root, threadKind, sha, "review threads", v)
}

// SaveAbout caches what the pull request says about itself.
func SaveAbout(root, sha string, v any) error {
	return saveCached(root, aboutKind, sha, "pull request's context", v)
}

// LoadAbout reads that context back into v, and reports nothing for a review
// staged before it was fetched.
func LoadAbout(root, sha string, v any) error {
	return loadCached(root, aboutKind, sha, "pull request's context", v)
}

func cachePath(root, kind, sha string) string {
	return filepath.Join(root, Dir, kind, sha+".json")
}

func saveCached(root, kind, sha, what string, v any) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding the %s: %w", what, err)
	}

	path := cachePath(root, kind, sha)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

func loadCached(root, kind, sha, what string, v any) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	body, err := os.ReadFile(cachePath(root, kind, sha))
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("reading the cached %s: %w", what, err)
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("reading the cached %s: %w", what, err)
	}

	return nil
}
