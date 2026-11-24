package artifact_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/aragonite/seen"

	"github.com/kyleking/second-look/internal/artifact"
)

// The fixture is the read-marks file second-look v0.57.3 wrote with its own
// internal/seen, every other hunk of the patch beside it marked. Read marks now
// come from aragonite/seen, and a reader upgrading must find every mark where
// the last release left it, read back id for id and written back byte for byte.
func TestSeenPath_ReadsMarksTheLastReleaseWrote(t *testing.T) {
	t.Parallel()

	patch, err := os.ReadFile(filepath.Join("testdata", "seen-v0.57.3.patch.golden"))
	if err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(filepath.Join("testdata", "seen-v0.57.3.toml.golden"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	path := artifact.SeenPath(root, 2)

	if want := filepath.Join(root, ".second-look", "seen", "pr-2.toml"); path != want {
		t.Fatalf("SeenPath = %s, want %s where v0.57.3 kept it", path, want)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, written, 0o600); err != nil { //nolint:gosec // the test's own temp dir
		t.Fatal(err)
	}

	set, err := seen.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	live := seen.Parse(patch)
	for i, r := range live {
		if want := i%2 == 0; set.Has(r.ID) != want {
			t.Errorf("hunk %d of %s: read = %v, want %v", r.Hunk, r.Path, !want, want)
		}
	}

	if err := seen.Save(path, set, live); err != nil {
		t.Fatal(err)
	}

	again, err := os.ReadFile(path) //nolint:gosec // the test's own temp dir
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(again, written) {
		t.Errorf("saving the marks back changed the file:\n%s\nwant\n%s", again, written)
	}
}
