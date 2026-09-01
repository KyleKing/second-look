package seen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// Set is which hunks have been read. It is keyed by content, so it is not tied
// to a head commit the way the cached diff and the prepared review are.
type Set struct {
	ids map[ID]bool
}

// record is one entry on disk. The hash is what matters; the path and the
// header are there so a person reading the file can tell what it refers to.
type record struct {
	Hash   string `toml:"hash"`
	Path   string `toml:"path"`
	Header string `toml:"header,omitempty"`
}

type file struct {
	Hunk []record `toml:"hunk"`
}

// Path is where a pull request's read hunks are kept. It is keyed by pull
// request rather than by head commit, because surviving a new head is the one
// thing this file exists to do.
func Path(root string, number int) string {
	return filepath.Join(root, ".second-look", "seen", fmt.Sprintf("pr-%d.toml", number))
}

// Load reads the set. A missing file is an empty set, not an error: a review
// nobody has read yet is the normal first case.
func Load(path string) (*Set, error) {
	s := &Set{ids: map[ID]bool{}}

	raw, err := os.ReadFile(path) //nolint:gosec // the repo root plus a pull request number
	if os.IsNotExist(err) {
		return s, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var f file

	dec := toml.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	for _, r := range f.Hunk {
		s.ids[ID(r.Hash)] = true
	}

	return s, nil
}

// Save writes the set, keeping only the hunks the given diff still carries.
// A mark for a hunk that no longer exists is dead weight, and a file that grows
// forever is one nobody can read.
func Save(path string, s *Set, live []Ref) error {
	var f file

	for _, r := range live {
		if !s.ids[r.ID] {
			continue
		}

		f.Hunk = append(f.Hunk, record{Hash: string(r.ID), Path: r.Path})
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	body, err := toml.Marshal(f)
	if err != nil {
		return fmt.Errorf("encoding the read hunks: %w", err)
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// Has reports whether a hunk has been read.
func (s *Set) Has(id ID) bool { return s.ids[id] }

// Toggle flips one hunk and reports what it became.
func (s *Set) Toggle(id ID) bool {
	if s.ids[id] {
		delete(s.ids, id)

		return false
	}

	s.ids[id] = true

	return true
}

// Mark sets a run of hunks to read or unread at once, which is what marking a
// whole file does.
func (s *Set) Mark(read bool, ids ...ID) {
	for _, id := range ids {
		if read {
			s.ids[id] = true

			continue
		}

		delete(s.ids, id)
	}
}

// Count is how many of the given hunks have been read.
func (s *Set) Count(live []Ref) int {
	n := 0

	for _, r := range live {
		if s.ids[r.ID] {
			n++
		}
	}

	return n
}
