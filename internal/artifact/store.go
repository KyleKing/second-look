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

func joinErrs(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	msgs := make([]string, len(errs))
	for i, err := range errs {
		msgs[i] = err.Error()
	}

	return errors.New(strings.Join(msgs, "\n"))
}

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
		return nil, err
	}

	var r Review
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&r); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("%s: %s", path, strict.String())
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
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	data, err := toml.Marshal(r)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".pr-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()

		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
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
	for _, c := range r.Comments {
		if c.Status == StatusDraft {
			out = append(out, c)
		}
	}

	return out
}
