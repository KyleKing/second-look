package threads_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/threads"
)

// Cassette is the one recorded interaction in this package: the GraphQL query
// for a pull request's open review threads. It reads and posts nothing, so
// re-recording it is safe in a way re-recording the review that posted is not:
//
//	GHCASSETTE_RECORD=1 go test ./internal/threads/
//
// cmd/second-look replays the same file, because `second-look get` makes this
// call and no scratch repository can record the rest of a get.
func Cassette(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("testdata", "cassettes", "threads.golden"))
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// Fetch runs gh in this process, so the cassette reaches it through the
// process environment rather than a child's. That rules out t.Parallel here.
func TestFetchReadsTheOpenThreads(t *testing.T) {
	s := ghcassette.Start(t, Cassette(t))
	for _, kv := range s.Env(t) {
		if name, value, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, "GH_CASSETTE") {
			t.Setenv(name, value)
		}
	}

	t.Setenv("PATH", filepath.Dir(s.GH())+string(os.PathListSeparator)+os.Getenv("PATH"))

	open, err := threads.Fetch(t.Context(), t.TempDir(), "KyleKing", "second-look", 2)
	if err != nil {
		t.Fatalf("fetching the threads on #2: %v", err)
	}

	if len(open) == 0 {
		t.Fatal("the recording carries no open thread, so nothing here is exercised")
	}

	for i := range open {
		if open[i].ReplyTo() == 0 {
			t.Errorf("thread %d carries no comment id, so nothing can answer it", i)
		}

		if open[i].Path == "" || open[i].Line == 0 {
			t.Errorf("thread %d anchors nowhere: %+v", i, open[i])
		}
	}

	s.RequireAllPlayed(t)
}
