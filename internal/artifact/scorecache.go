package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ScorePath is where the review screen leaves what it rated a change, keyed by
// head commit for the same reason the diff is: the rating is about that diff.
func ScorePath(root, sha string) string {
	return filepath.Join(root, Dir, "score", sha+".json")
}

// Cost is what a run made of a change: the review-cost rating, and how many
// lines the change asks somebody to read.
type Cost struct {
	Total   int `json:"total"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// Measured reports whether the size is one somebody counted. Nothing rates a
// diff that changes no lines, so zero on both sides is an older file rather
// than an empty change.
func (c Cost) Measured() bool { return c.Added > 0 || c.Removed > 0 }

// SaveScore records a rating the screen has already worked out. It is a
// convenience rather than state: the queue reads it to order rows without
// fetching a diff for each of them, and a run that never opened the review
// simply has no number to show.
func SaveScore(root, sha string, c Cost) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	body, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding the rating: %w", err)
	}

	path := ScorePath(root, sha)
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// LoadScore reads a rating left by an earlier run, reporting whether there was
// one. Nothing here fails: a queue that cannot read a cached number shows no
// number, which is what a queue of pull requests nobody has opened looks like.
func LoadScore(root, sha string) (Cost, bool) {
	if err := checkSHA(sha); err != nil {
		return Cost{}, false
	}

	body, err := os.ReadFile(ScorePath(root, sha))
	if err != nil {
		return Cost{}, false
	}

	var out Cost
	if err := json.Unmarshal(body, &out); err != nil {
		return Cost{}, false
	}

	return out, true
}
