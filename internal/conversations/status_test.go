package conversations_test

import (
	"path/filepath"
	"testing"

	"github.com/kyleking/second-look/internal/conversations"
)

// A prompt reads the counts the queue last wrote, so a save followed by a load
// hands over the same numbers, and a file nobody wrote is an empty status
// rather than an error.
func TestStatusSurvivesARoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "status.toml")

	got, err := conversations.LoadStatus(path)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Updated.IsZero() || got.Unread != 0 || got.Open != 0 {
		t.Errorf("a missing file read as %+v", got)
	}

	want := conversations.Status{Updated: at("2026-09-01T12:00:00Z"), Unread: 3, Open: 5}
	if err := conversations.SaveStatus(path, want); err != nil {
		t.Fatal(err)
	}

	got, err = conversations.LoadStatus(path)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Updated.Equal(want.Updated) || got.Unread != want.Unread || got.Open != want.Open {
		t.Errorf("the file held %+v, want %+v", got, want)
	}
}
