// Package jjseen shares a review's read marks with jojo and jj-diff, which file them per jj change.
//
// jojo keeps one set of read hunks per change, named by the change's full id, under its own state
// directory for the repository, and jj-diff reads the same store. A hunk is named by its content
// (aragonite's seen), so a hunk second-look shows matches jojo's mark for it exactly when it reads
// the same in the change's own diff: always for a review of one change, and for a span of several
// wherever no two of them touch the same lines.
//
// second-look keeps its own set as the one it answers from and writes to its own file. Marks flow
// between the two at the edges: Import lays jojo's marks over the review's without copying them
// into its file, and Export writes back what the review changed since, so a mark jojo makes
// meanwhile is never overwritten by a stale answer.
package jjseen

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/seen"
)

// App is the state directory the marks live under: jojo's, because jojo is where they are made and
// every tool must name one directory for a repository.
const App = seen.JJApp

// ErrNotJJ is a directory in no jj workspace, which has no change ids to share marks by.
var ErrNotJJ = errors.New("not in a jj workspace")

// Change is one jj change a review covers: its full change id and the hunks of its own diff, which
// is what jojo names its marks by.
type Change struct {
	ID    string
	Hunks []seen.Ref
}

// Marks is jojo's store for the review's changes, with what the review last agreed with it.
type Marks struct {
	store *seen.Store
	last  map[seen.ID]bool
	// migrate is why marks under an older id could not be moved, which Import reports: the review
	// still opens, short those marks.
	migrate error
	ids     []string
	changes []Change
}

// Open is jojo's store for the jj repository dir is in, for changes. A change filed under an older,
// shorter id is moved to its full one first, as jojo itself does; a move that fails is Import's to
// report, since the review opens without those marks rather than not at all.
func Open(dir string, changes []Change) (*Marks, error) {
	store, err := seen.OpenJJ(dir)
	if errors.Is(err, seen.ErrNoJJ) {
		return nil, ErrNotJJ
	}

	if err != nil {
		return nil, fmt.Errorf("finding jojo's read marks: %w", err)
	}

	k := &Marks{store: store, changes: changes, last: map[seen.ID]bool{}}

	for _, c := range changes {
		k.ids = append(k.ids, c.ID)

		if _, err := store.Migrate(c.ID); err != nil {
			k.migrate = errors.Join(k.migrate, fmt.Errorf("moving %s's older read marks: %w", c.ID, err))
		}
	}

	return k, nil
}

// Changes is how many changes the marks are shared across.
func (k *Marks) Changes() int { return len(k.changes) }

// Import lays over s every hunk jojo has marked read for one of the changes, as read but never
// written to s's own file (seen.Set.Include), and answers how many hunks of live it newly shows as
// read. It reads jojo's files again each time, so a later Import picks up marks jojo made since.
// A hunk read in s stays read, and one s has changed and not yet saved keeps its state. What s says
// afterwards about each change's hunks is what Export compares against.
//
// A save of s keeps only what is in s's own file, so a caller imports again after every save.
func (k *Marks) Import(s *seen.Set, live []seen.Ref) (int, error) {
	errs := k.migrate
	k.migrate = nil

	for _, id := range k.ids {
		k.store.Reload(id)
	}

	theirs, err := k.store.Union(k.ids...)
	if err != nil {
		return 0, errors.Join(errs, fmt.Errorf("reading jojo's read marks: %w", err))
	}

	n := 0

	for _, r := range live {
		if theirs.Has(r.ID) && !s.Has(r.ID) {
			n++
		}
	}

	s.Include(theirs)
	k.remember(s)

	return n, errs
}

// Export writes to jojo's store each change's hunk whose read state in s changed since Import or the
// last Export, and only those, so a hunk jojo marked meanwhile keeps its mark. Call it before s is
// saved: a save drops what Import laid over s, which would read as the reader unmarking it.
func (k *Marks) Export(s *seen.Set) error {
	var errs error

	for _, c := range k.changes {
		var read, unread []seen.ID

		for _, r := range c.Hunks {
			now := s.Has(r.ID)
			if was, known := k.last[r.ID]; known && was == now {
				continue
			}

			if now {
				read = append(read, r.ID)
			} else {
				unread = append(unread, r.ID)
			}
		}

		for _, side := range []struct {
			ids  []seen.ID
			read bool
		}{{read, true}, {unread, false}} {
			if len(side.ids) == 0 {
				continue
			}

			if err := k.store.Mark(c.ID, side.read, c.Hunks, side.ids...); err != nil {
				errs = errors.Join(errs, fmt.Errorf("writing %s's read marks for jojo: %w", c.ID, err))
			}
		}
	}

	k.remember(s)

	return errs
}

// remember records what s says about every change's hunks, which is what the next Export diffs
// against.
func (k *Marks) remember(s *seen.Set) {
	for _, c := range k.changes {
		for _, r := range c.Hunks {
			k.last[r.ID] = s.Has(r.ID)
		}
	}
}

// headLimit is how many of a head's changes ForHead reads, each a jj call. A pull request past it
// shares marks for its newest changes only.
const headLimit = 50

// ForHead is jojo's record for the changes a pull request's head spans in dir: every change between
// trunk() and the commit head, read as Read does. It is nil with no error where dir's jj repository
// does not hold head, and ErrNotJJ where dir is in none.
func ForHead(ctx context.Context, dir, head string) (*Marks, error) {
	if _, err := seen.JJRepo(dir); errors.Is(err, seen.ErrNoJJ) {
		return nil, ErrNotJJ
	}

	changes, err := Read(ctx, dir, "trunk()..present("+head+")", headLimit)
	if err != nil {
		return nil, err
	}

	if len(changes) == 0 {
		return nil, nil //nolint:nilnil // the repository does not hold the head, which is no error
	}

	return Open(dir, changes)
}

// field separates the fields Read asks jj's log for, a byte no change id or commit id holds.
const field = "\x1f"

// Read lists the changes revset names in root, each with the hunks of its own diff, as jojo reads
// them (`jj diff -r <commit> --git`). At most limit changes are read, since each costs a jj call;
// a revset naming more answers the newest limit.
func Read(ctx context.Context, root, revset string, limit int) ([]Change, error) {
	out, err := jj(ctx, root, "log", "--no-graph", "--ignore-working-copy", "--limit", strconv.Itoa(limit),
		"-r", revset, "-T", `change_id ++ "`+field+`" ++ commit_id ++ "\n"`)
	if err != nil {
		return nil, err
	}

	var changes []Change

	for line := range strings.Lines(out) {
		id, commit, ok := strings.Cut(strings.TrimSpace(line), field)
		if !ok {
			continue
		}

		patch, err := jj(ctx, root, "diff", "--git", "--ignore-working-copy", "-r", commit)
		if err != nil {
			return nil, err
		}

		changes = append(changes, Change{ID: id, Hunks: seen.Hunks(diff.Parse([]byte(patch)))})
	}

	return changes, nil
}

func jj(ctx context.Context, root string, args ...string) (string, error) {
	argv := append([]string{"--no-pager", "--color=never"}, args...)
	cmd := exec.CommandContext(ctx, "jj", argv...) // #nosec G204 -- jj, a revset, and an id
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("jj %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
		}

		return "", fmt.Errorf("jj %s: %w", strings.Join(args, " "), err)
	}

	return string(out), nil
}
