// Package lease is the claim a sitting puts on a checkout. Reviewing a
// repository needs a working tree only when the diff or an agent is run in
// it, and a clone is one directory: a second second-look that moved the same
// clone onto another pull request would pull the floor out from under the
// first.
//
// The claim is a small file under the repository's state directory naming the
// path held and the pid holding it. Pid rather than a generated id is the
// bookkeeping, because it is what a reader can check: a lease whose process
// is gone is swept rather than believed, so a session that died without
// releasing leaves nothing behind.
package lease

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/kyleking/second-look/internal/artifact"
)

const (
	filePerm = 0o600
	dirPerm  = 0o750
)

// Record is one claim: the repository it is against, the checkout held, and
// the process holding it.
type Record struct {
	Repo     string    `toml:"repo"`
	Path     string    `toml:"path"`
	PID      int       `toml:"pid"`
	Acquired time.Time `toml:"acquired"`
}

// TakenError reports a checkout claimed by a live session of second-look.
type TakenError struct {
	Record Record
}

func (e *TakenError) Error() string {
	return e.Record.Path + " is leased by another second-look"
}

// errBadRepo names a repository string that is not owner/name, which is the
// only shape a lease can file under.
var errBadRepo = errors.New("a repository is owner/name")

// Held is a claim this process made. Release gives it back.
type Held struct {
	Record Record
	file   string
}

// key is the filename one claim gets. The path itself is a poor filename, so
// the file is named for it by hash.
func key(path string) string {
	sum := sha256.Sum256([]byte(path))

	return hex.EncodeToString(sum[:8]) + ".toml"
}

// dir is the directory a repository's claims live in.
func dir(host, repo string) (string, error) {
	owner, name, found := strings.Cut(repo, "/")
	if !found {
		return "", fmt.Errorf("lease: %q: %w", repo, errBadRepo)
	}

	root, err := artifact.StateRoot(host, owner, name)
	if err != nil {
		return "", fmt.Errorf("lease: %w", err)
	}

	return filepath.Join(root, "lease"), nil
}

// read parses one claim file, erroring on what is not one.
func read(file string) (Record, error) {
	data, err := os.ReadFile(file) //nolint:gosec // claim files live under the state root this package made
	if err != nil {
		return Record{}, fmt.Errorf("lease: %w", err)
	}

	var rec Record
	if err := toml.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("lease: %s: %w", filepath.Base(file), err)
	}

	return rec, nil
}

// Acquire claims path for this process. A live session already holding it
// gets a Taken; a dead one's record is removed first and the claim proceeds.
func Acquire(host, repo, path string) (*Held, error) {
	d, err := dir(host, repo)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(d, dirPerm); err != nil {
		return nil, fmt.Errorf("lease: %w", err)
	}

	rec := Record{Repo: repo, Path: path, PID: os.Getpid(), Acquired: time.Now().UTC()}

	body, err := toml.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("lease: %w", err)
	}

	target := filepath.Join(d, key(path))

	for range 2 {
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm) //nolint:gosec // same
		if err == nil {
			if _, err := f.Write(body); err != nil {
				_ = f.Close()         //nolint:errcheck // the write already failed
				_ = os.Remove(target) //nolint:errcheck // best-effort cleanup of a claim never taken

				return nil, fmt.Errorf("lease: %w", err)
			}

			if err := f.Close(); err != nil {
				_ = os.Remove(target) //nolint:errcheck // same

				return nil, fmt.Errorf("lease: %w", err)
			}

			return &Held{Record: rec, file: target}, nil
		}

		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("lease: %w", err)
		}

		cur, err := read(target)
		if err == nil && alive(cur.PID) {
			return nil, &TakenError{Record: cur}
		}

		// A record that cannot be read, or whose process is gone, is stale.
		_ = os.Remove(target) //nolint:errcheck // a stale record that stays gets swept again next read
	}

	// Losing the race twice means somebody else holds it now.
	return nil, &TakenError{Record: rec}
}

// Release gives the claim back. Releasing twice is fine: the file is already
// gone the second time.
func (h *Held) Release() {
	_ = os.Remove(h.file) //nolint:errcheck // already gone is the only failure worth ignoring, and there is no other
}

// List is the live claims against repo, the held path indexing each. A claim
// whose pid is dead is swept as it is read, so the map never lies about
// holding the checkout.
func List(host, repo string) map[string]Record {
	d, err := dir(host, repo)
	if err != nil {
		return nil
	}

	entries, err := os.ReadDir(d)
	if err != nil {
		return nil
	}

	out := make(map[string]Record, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}

		file := filepath.Join(d, e.Name())

		rec, err := read(file)
		if err != nil || !alive(rec.PID) {
			_ = os.Remove(file) //nolint:errcheck // a stale record that stays gets swept again next read

			continue
		}

		out[rec.Path] = rec
	}

	return out
}

// Ours is the claim this process holds on repo, nil where none is.
func Ours(host, repo string) *Held {
	for path, rec := range List(host, repo) {
		if rec.PID == os.Getpid() {
			d, err := dir(host, repo)
			if err != nil {
				return nil
			}

			return &Held{Record: rec, file: filepath.Join(d, key(path))}
		}
	}

	return nil
}

// ReleaseAll drops every claim this process holds, which is what ending a
// sitting owes: focus can move and hand a lease back mid-session, but leaving
// takes them all. What was released is returned so the caller can warn about
// work still staged on the repository.
func ReleaseAll() ([]Record, error) {
	home, err := artifact.StateHome()
	if err != nil {
		//nolint:wrapcheck // StateHome's own error already names what failed
		return nil, err
	}

	mine := os.Getpid()
	var out []Record
	var stale []string

	err = filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(filepath.Dir(path)) != "lease" ||
			!strings.HasSuffix(d.Name(), ".toml") {
			return nil //nolint:nilerr // a path that will not walk is a claim we cannot sweep either way
		}

		if rec, err := read(path); err == nil && rec.PID == mine {
			out = append(out, rec)
			stale = append(stale, path)
		}

		return nil
	})

	for _, path := range stale {
		_ = os.Remove(path) //nolint:errcheck // a claim that stays gets swept by the next List
	}

	if err != nil {
		return out, fmt.Errorf("lease: %w", err)
	}

	return out, nil
}
