package artifact_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
)

// TestContextCacheRoundTrips holds both kinds of context to the same contract:
// what was saved reads back, a head with nothing cached reads as nothing, and a
// file that is there but unreadable is an error rather than an empty answer.
func TestContextCacheRoundTrips(t *testing.T) {
	t.Parallel()

	kinds := []cacheKind{
		{"threads", artifact.ThreadsPath, artifact.SaveThreads, artifact.LoadThreads},
		{"about", artifact.AboutPath, artifact.SaveAbout, artifact.LoadAbout},
	}

	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			checkCacheKind(t, k)
		})
	}
}

// cacheKind is one kind of context cached against a head.
type cacheKind struct {
	name string
	path func(root, sha string) string
	save func(root, sha string, v any) error
	load func(root, sha string, v any) error
}

func checkCacheKind(t *testing.T, k cacheKind) {
	t.Helper()

	root := t.TempDir()
	want := []string{"first", "second"}

	if err := k.save(root, "abc123", want); err != nil {
		t.Fatal(err)
	}

	if p := k.path(root, "abc123"); p != filepath.Join(root, artifact.Dir, k.name, "abc123.json") {
		t.Errorf("cached at %s", p)
	}

	var got []string
	if err := k.load(root, "abc123", &got); err != nil || !slices.Equal(got, want) {
		t.Errorf("read back %v (%v), want %v", got, err, want)
	}

	kept := []string{"untouched"}
	if err := k.load(root, "def456", &kept); err != nil || !slices.Equal(kept, []string{"untouched"}) {
		t.Errorf("an uncached head answered %v (%v), want nothing and no error", kept, err)
	}

	if err := os.WriteFile(k.path(root, "abc123"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := k.load(root, "abc123", &got); err == nil {
		t.Error("an unparsable cache read as fine")
	}

	if err := os.Remove(k.path(root, "abc123")); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(k.path(root, "abc123"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := k.load(root, "abc123", &got); err == nil {
		t.Error("a cache that cannot be read answered nothing rather than an error")
	}

	for sha, wantErr := range map[string]error{"": artifact.ErrNoHeadSHA, "../up": artifact.ErrNotASHA} {
		if err := k.save(root, sha, want); !errors.Is(err, wantErr) {
			t.Errorf("saving under %q answered %v, want %v", sha, err, wantErr)
		}

		if err := k.load(root, sha, &got); !errors.Is(err, wantErr) {
			t.Errorf("loading under %q answered %v, want %v", sha, err, wantErr)
		}
	}
}
