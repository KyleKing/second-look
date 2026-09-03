package artifact_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
)

// The queue's ratings are one file for every repository, so what matters is
// that a round trip keeps the update time each cost was rated at: that is the
// whole of the invalidation, and a time that came back wrong would order a
// queue by a rating of a diff nobody can see any more.
//
// It cannot be parallel: it points HOME at a directory of its own.
func TestRatingsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if got := artifact.LoadRatings(); len(got) != 0 {
		t.Errorf("a home with no ratings answered %v", got)
	}

	at := time.Date(2026, time.September, 1, 12, 30, 0, 0, time.UTC)
	want := artifact.Ratings{
		"kyleking/second-look#2": {Updated: at, Cost: 38, Rated: true},
		"octocat/hello-world#9":  {Updated: at.AddDate(0, 0, -3)},
	}

	if err := artifact.SaveRatings(want); err != nil {
		t.Fatal(err)
	}

	got := artifact.LoadRatings()
	if len(got) != len(want) {
		t.Fatalf("read back %d ratings, want %d", len(got), len(want))
	}

	for key, w := range want {
		if g := got[key]; g.Cost != w.Cost || g.Rated != w.Rated || !g.Updated.Equal(w.Updated) {
			t.Errorf("%s came back %+v, want %+v", key, g, w)
		}
	}

	// A file nothing can read is no ratings rather than an error: the queue
	// orders itself by age without them and works.
	path, err := artifact.RatingsPath()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("this is not toml = = ="), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := artifact.LoadRatings(); len(got) != 0 {
		t.Errorf("an unreadable file answered %v", got)
	}
}

// A cost worked out a different way makes every cached number mean something
// else, and a queue ordering a mix of two scales orders wrongly without saying
// so. A file from another scale is no ratings, which is what the queue already
// handles.
func TestRatingsFromAnotherScaleAreDropped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path, err := artifact.RatingsPath()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	const row = "\n[[rated]]\nkey = \"o/r#1\"\nupdated = 2026-01-01T00:00:00Z\ncost = 99\nrated = true\n"

	body := fmt.Sprintf("scale = %d\n%s", artifact.RatingScale-1, row)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := artifact.LoadRatings(); len(got) != 0 {
		t.Errorf("ratings from an older scale were read: %v", got)
	}

	// The same file on this scale reads, so what the guard rejects is the scale
	// rather than the shape.
	current := strings.Replace(body,
		fmt.Sprintf("scale = %d", artifact.RatingScale-1),
		fmt.Sprintf("scale = %d", artifact.RatingScale), 1)
	if err := os.WriteFile(path, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := artifact.LoadRatings(); len(got) != 1 {
		t.Errorf("ratings on this scale were dropped too: %v", got)
	}
}
