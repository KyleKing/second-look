package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/second-look/internal/config"
)

func write(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// The sections a queue is worth having are the ones a person writes, so the
// file is read in the order it names them and nothing is merged into it.
func TestLoadReadsSectionsInOrder(t *testing.T) {
	t.Parallel()

	path := write(t, `
limit = 25

[[section]]
name = "my work"
query = "author:@me org:acme is:open sort:updated-desc"

[[section]]
name = "needs my review"
query = "review-requested:@me is:open"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Limit != 25 {
		t.Errorf("Limit = %d, want 25", cfg.Limit)
	}

	if len(cfg.Sections) != 2 {
		t.Fatalf("%d section(s), want 2", len(cfg.Sections))
	}

	if cfg.Sections[0].Name != "my work" || cfg.Sections[1].Name != "needs my review" {
		t.Errorf("sections came back as %+v", cfg.Sections)
	}
}

// A missing file is the built-in buckets rather than an error: nobody should
// have to write a file to get a queue.
func TestLoadOnNoFile(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(filepath.Join(t.TempDir(), "never-written.toml"))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sections) != 0 || cfg.Limit != 0 {
		t.Errorf("a missing file yielded %+v", cfg)
	}
}

// A file that exists and says something wrong stops the run. The alternative is
// a queue quietly showing the built-in buckets while the file it was written
// into is ignored.
func TestLoadRefusesWhatCannotBeRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{
			name: "a section with no query",
			body: "[[section]]\nname = \"mine\"\n",
			want: config.ErrNoQuery,
		},
		{
			name: "a section with no name",
			body: "[[section]]\nquery = \"is:open\"\n",
			want: config.ErrNoName,
		},
		{
			name: "gh-dash's own keys",
			body: "prSections = []\n",
			want: config.ErrUnknownKey,
		},
		{
			name: "a misspelled key",
			body: "[[section]]\nname = \"mine\"\nfilters = \"is:open\"\n",
			want: config.ErrUnknownKey,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(write(t, tc.body))
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The config belongs beside gh's and gh-dash's rather than in the platform's
// application-support directory, because a person writes it by hand and keeps it
// in their dotfiles.
func TestPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/.config")

	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}

	if want := "/somewhere/.config/second-look/config.toml"; path != want {
		t.Errorf("Path = %q, want %q", path, want)
	}
}
