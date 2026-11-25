package jjseen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/seen"

	"github.com/kyleking/second-look/internal/jjseen"
)

// fixture is a jj repository with two described changes over a base, each adding a file, so each
// change's own diff is one hunk.
type fixture struct {
	dir     string
	changes []string
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}

	dir := t.TempDir()
	run(t, dir, "git", "init")
	write(t, dir, "a.txt", "one\n")
	run(t, dir, "describe", "-m", "base")
	run(t, dir, "bookmark", "create", "base", "-r", "@")

	f := fixture{dir: dir}

	for _, step := range []string{"b", "c"} {
		run(t, dir, "new")
		write(t, dir, step+".txt", step+"\n")
		run(t, dir, "describe", "-m", "add "+step)
		f.changes = append(f.changes, run(t, dir, "log", "--no-graph", "-r", "@", "-T", "change_id"))
	}

	return f
}

// jojoStore is the store jojo keeps for the fixture's repository, the one jjseen must read.
func (f fixture) jojoStore(t *testing.T) *seen.Store {
	t.Helper()

	repo, err := seen.JJRepo(f.dir)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := seen.StateDir(jjseen.App, "seen", seen.RepoKey(repo))
	if err != nil {
		t.Fatal(err)
	}

	return seen.Open(dir)
}

// ownHunks is a change's own diff as jojo names it: jj diff -r <change> --git through seen.Parse.
func (f fixture) ownHunks(t *testing.T, change string) []seen.Ref {
	t.Helper()

	return seen.Parse([]byte(run(t, f.dir, "diff", "-r", change, "--git")))
}

// jojoMarks marks every hunk of change read the way jojo saves it.
func (f fixture) jojoMarks(t *testing.T, change string) []seen.Ref {
	t.Helper()

	store, hunks := f.jojoStore(t), f.ownHunks(t, change)

	set, err := store.Set(change)
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range hunks {
		set.Mark(true, h.ID)
	}

	if err := store.Save(change, hunks); err != nil {
		t.Fatal(err)
	}

	return hunks
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()

	argv := append([]string{"--no-pager", "--color=never"}, args...)
	cmd := exec.CommandContext(t.Context(), "jj", argv...) // #nosec G204 -- a fixture's own constants
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "JJ_USER=test", "JJ_EMAIL=test@example.com")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jj %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRead_NamesEachChangesHunksAsJojoDoes is the hashing half of "read 0 of 11": jjseen reads each
// change's own diff, and its hunk ids are exactly the ones jojo files marks under.
func TestRead_NamesEachChangesHunksAsJojoDoes(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	changes, err := jjseen.Read(t.Context(), f.dir, "base..@", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != len(f.changes) {
		t.Fatalf("read %d changes, want %d", len(changes), len(f.changes))
	}

	for _, c := range changes {
		want := f.ownHunks(t, c.ID)
		if len(c.Hunks) != len(want) || c.Hunks[0].ID != want[0].ID {
			t.Errorf("change %s reads as %+v, jojo as %+v", c.ID, c.Hunks, want)
		}
	}
}

// TestImport_MarksWhatJojoRead is the scope half: marks jojo filed by change id are read into the
// review's own set, for every change the review spans.
func TestImport_MarksWhatJojoRead(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	changes := make([]jjseen.Change, 0, len(f.changes))

	var live []seen.Ref

	for _, id := range f.changes {
		hunks := f.jojoMarks(t, id)
		changes = append(changes, jjseen.Change{ID: id, Hunks: hunks})
		live = append(live, hunks...)
	}

	marks, err := jjseen.Open(f.dir, changes)
	if err != nil {
		t.Fatal(err)
	}

	read := seen.New()

	n, err := marks.Import(read, live)
	if err != nil {
		t.Fatal(err)
	}

	if n != len(live) || read.Count(live) != len(live) {
		t.Errorf("imported %d, the review reads %d of %d", n, read.Count(live), len(live))
	}
}

// TestExport_WritesOnlyWhatTheReviewChanged keeps a mark jojo made while the review was open: the
// review marks one change's hunk, jojo the other's meanwhile, and both survive the review's save.
func TestExport_WritesOnlyWhatTheReviewChanged(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	first, second := f.ownHunks(t, f.changes[0]), f.ownHunks(t, f.changes[1])

	marks, err := jjseen.Open(f.dir, []jjseen.Change{
		{ID: f.changes[0], Hunks: first}, {ID: f.changes[1], Hunks: second},
	})
	if err != nil {
		t.Fatal(err)
	}

	read := seen.New()
	if _, err := marks.Import(read, append(append([]seen.Ref{}, first...), second...)); err != nil {
		t.Fatal(err)
	}

	f.jojoMarks(t, f.changes[1])
	read.Mark(true, first[0].ID)

	if err := marks.Export(read); err != nil {
		t.Fatal(err)
	}

	again := f.jojoStore(t)

	for i, want := range []struct {
		change string
		hunks  []seen.Ref
	}{{f.changes[0], first}, {f.changes[1], second}} {
		set, err := again.Set(want.change)
		if err != nil {
			t.Fatal(err)
		}

		if !set.Has(want.hunks[0].ID) {
			t.Errorf("change %d's hunk is not read in jojo's store after the review saved", i+1)
		}
	}

	// Taking the mark back is written through too.
	read.Mark(false, first[0].ID)

	if err := marks.Export(read); err != nil {
		t.Fatal(err)
	}

	set, err := f.jojoStore(t).Set(f.changes[0])
	if err != nil {
		t.Fatal(err)
	}

	if set.Has(first[0].ID) {
		t.Error("a hunk the review marked unread is still read in jojo's store")
	}
}

// TestForHead_AHeadTheRepositoryLacksSharesNothing is a pull request reviewed from a jj checkout
// that never fetched its head: no record, and no error.
func TestForHead_AHeadTheRepositoryLacksSharesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	marks, err := jjseen.ForHead(t.Context(), f.dir, strings.Repeat("ab", 20))
	if err != nil || marks != nil {
		t.Errorf("ForHead on an absent head answered %v, %v", marks, err)
	}

	tip := run(t, f.dir, "log", "--no-graph", "-r", "@", "-T", "commit_id")

	marks, err = jjseen.ForHead(t.Context(), f.dir, tip)
	if err != nil || marks == nil || marks.Changes() != len(f.changes)+1 {
		t.Errorf("ForHead on the tip answered %v, %v; want the base and both changes", marks, err)
	}
}

// TestOpen_OutsideJJ is a git checkout, which has no change ids to share by.
func TestOpen_OutsideJJ(t *testing.T) {
	t.Parallel()

	if _, err := jjseen.ForHead(t.Context(), t.TempDir(), "abc"); err == nil {
		t.Error("ForHead outside a jj workspace answered no error")
	}
}
