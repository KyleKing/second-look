package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Dir is the artifact directory, relative to the repository root.
const Dir = ".second-look"

const dirPerm = 0o750

// Path is where the review for a pull request is kept.
func Path(root string, number int) string {
	return filepath.Join(root, Dir, fmt.Sprintf("pr-%d.toml", number))
}

// Load reads and validates a review. An unknown key is an error rather than a
// silent drop: a hand-edit that misspells a field should say so, and a field the
// schema does not know is a field the posting allowlist cannot classify.
func Load(path string) (*Review, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is built from the repo root and a PR number
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var r Review
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&r); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("%s: %w\n%s", path, ErrUnknownKey, strict.String())
		}

		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}

	return &r, nil
}

// Save writes the review, replacing the file atomically so an interrupted write
// cannot leave a half-parsed review behind.
func Save(path string, r *Review) error {
	if err := r.Validate(); err != nil {
		return fmt.Errorf("refusing to write %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	data, err := toml.Marshal(r)
	if err != nil {
		return fmt.Errorf("encoding the review: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".pr-*.toml")
	if err != nil {
		return fmt.Errorf("creating a temporary file beside %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // the rename below already moved it on the happy path

	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck,gosec // the write already failed and the file is removed below

		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp.Name(), err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	return nil
}

// Upsert adds a comment, or replaces the one already carrying its id.
func (r *Review) Upsert(c Comment) {
	for i := range r.Comments {
		if c.ID != "" && r.Comments[i].ID == c.ID {
			r.Comments[i] = c

			return
		}
	}

	r.Comments = append(r.Comments, c)
}

// Drafts returns the comments still marked draft. Posting refuses while any
// remain, so an unfinished thought is never published and never quietly dropped.
func (r *Review) Drafts() []Comment {
	var out []Comment

	for i := range r.Comments {
		if r.Comments[i].Status == StatusDraft {
			out = append(out, r.Comments[i])
		}
	}

	return out
}
