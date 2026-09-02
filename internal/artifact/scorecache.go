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

// SaveScore records a rating the screen has already worked out. It is a
// convenience rather than state: the queue reads it to order rows without
// fetching a diff for each of them, and a run that never opened the review
// simply has no number to show.
func SaveScore(root, sha string, total int) error {
	if err := checkSHA(sha); err != nil {
		return err
	}

	body, err := json.Marshal(map[string]int{"total": total})
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
func LoadScore(root, sha string) (int, bool) {
	if err := checkSHA(sha); err != nil {
		return 0, false
	}

	body, err := os.ReadFile(ScorePath(root, sha))
	if err != nil {
		return 0, false
	}

	var out struct {
		Total int `json:"total"`
	}

	if err := json.Unmarshal(body, &out); err != nil {
		return 0, false
	}

	return out.Total, true
}
