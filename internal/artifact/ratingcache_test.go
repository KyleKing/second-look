package artifact_test

import (
	"os"
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
