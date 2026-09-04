package artifact_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
)

// Adopt is what makes a pull request read from two clones one review: whatever
// a working copy still holds is moved into the store, the emptied tree is left
// with a pointer, and running it again finds nothing to do.
func TestAdoptMovesAWorkingCopyIntoTheStore(t *testing.T) {
	t.Parallel()

	from, to := t.TempDir(), t.TempDir()

	seed(t, filepath.Join(from, artifact.Dir, "pr-2.toml"), "version = 1\n")
	seed(t, filepath.Join(from, artifact.Dir, "diff", "abc.patch"), "a patch\n")

	for range 2 {
		if err := artifact.Adopt(from, to); err != nil {
			t.Fatalf("adopting: %v", err)
		}
	}

	for _, rel := range []string{"pr-2.toml", filepath.Join("diff", "abc.patch")} {
		if _, err := os.Stat(filepath.Join(to, artifact.Dir, rel)); err != nil {
			t.Errorf("%s did not move into the store: %v", rel, err)
		}

		if _, err := os.Stat(filepath.Join(from, artifact.Dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s is still in the working copy: %v", rel, err)
		}
	}

	if _, err := os.Stat(filepath.Join(from, artifact.Dir, "diff")); !os.IsNotExist(err) {
		t.Error("the emptied cache directory was left behind")
	}

	if _, err := os.Stat(filepath.Join(from, artifact.Dir, artifact.Pointer)); err != nil {
		t.Errorf("nothing says where the reviews went: %v", err)
	}
}

// A file on both sides is staged work in two places and picking one would drop
// the other, so it stays put. It used to stop the whole migration with it,
// which hid every other review behind one duplicate.
func TestAdoptLeavesATwiceStagedFileAndMovesTheRest(t *testing.T) {
	t.Parallel()

	from, to := t.TempDir(), t.TempDir()

	seed(t, filepath.Join(from, artifact.Dir, "pr-2.toml"), "from the checkout\n")
	seed(t, filepath.Join(to, artifact.Dir, "pr-2.toml"), "from the store\n")
	seed(t, filepath.Join(from, artifact.Dir, "pr-3.toml"), "only here\n")

	if err := artifact.Adopt(from, to); err != nil {
		t.Fatalf("adopting over a staged review: %v", err)
	}

	if body := read(t, filepath.Join(to, artifact.Dir, "pr-2.toml")); body != "from the store\n" {
		t.Errorf("the store's copy was overwritten with %q", body)
	}

	if body := read(t, filepath.Join(from, artifact.Dir, "pr-2.toml")); body != "from the checkout\n" {
		t.Errorf("the checkout's copy became %q rather than being left alone", body)
	}

	if _, err := os.Stat(filepath.Join(from, artifact.Dir, "pr-3.toml")); !os.IsNotExist(err) {
		t.Error("the review with no copy in the store was held back by the duplicate")
	}

	if body := read(t, filepath.Join(to, artifact.Dir, "pr-3.toml")); body != "only here\n" {
		t.Errorf("pr-3 reached the store as %q", body)
	}

	// Saying nothing here is read any more would be wrong while pr-2 still is.
	if _, err := os.Stat(filepath.Join(from, artifact.Dir, artifact.Pointer)); !os.IsNotExist(err) {
		t.Error("a directory still holding staged work was marked as moved")
	}
}

// AdoptHere reads the store out of the reviews themselves, which is what a
// command holding no target can do.
//
// Not parallel: it points HOME at a store of its own, and the environment is
// the whole process's.
//
//nolint:paralleltest // atHome sets HOME for the whole process
func TestAdoptHereMovesWhatTheReviewsName(t *testing.T) {
	dir := t.TempDir()
	seed(t, filepath.Join(dir, artifact.Dir, "pr-2.toml"), staged("acme", "widget"))
	atHome(t)

	if err := artifact.AdoptHere(dir); err != nil {
		t.Fatalf("adopting: %v", err)
	}

	store, err := artifact.StateRoot("github.com", "acme", "widget")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(artifact.Path(store, 2)); err != nil {
		t.Errorf("the review is not in the store: %v", err)
	}
}

// A directory staging two repositories names no single store. Not parallel,
// for the reason above.
//
//nolint:paralleltest // atHome sets HOME for the whole process
func TestAdoptHereRefusesTwoRepositories(t *testing.T) {
	dir := t.TempDir()
	seed(t, filepath.Join(dir, artifact.Dir, "pr-2.toml"), staged("acme", "widget"))
	seed(t, filepath.Join(dir, artifact.Dir, "pr-3.toml"), staged("acme", "other"))
	atHome(t)

	if err := artifact.AdoptHere(dir); !errors.Is(err, artifact.ErrTwoRepos) {
		t.Fatalf("adopting a directory staging two repositories: %v", err)
	}
}

// A review nothing can read names no repository, so nothing can move it.
func TestAdoptHereLeavesAReviewNothingCanRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	seed(t, filepath.Join(dir, artifact.Dir, "pr-2.toml"), "not toml at all\x00")

	if err := artifact.AdoptHere(dir); err != nil {
		t.Fatalf("adopting: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, artifact.Dir, "pr-2.toml")); err != nil {
		t.Errorf("a review nothing can read was moved somewhere: %v", err)
	}
}

// atHome points the state directory at one of the test's own, on every platform
// os.UserConfigDir answers for.
func atHome(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func staged(owner, name string) string {
	return "version = 1\nhost = \"github.com\"\nowner = \"" + owner + "\"\nrepo = \"" + name +
		"\"\nnumber = 2\nevent = \"COMMENT\"\nbody = \"\"\nnote = \"\"\n"
}

func seed(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // a path this test wrote
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}
