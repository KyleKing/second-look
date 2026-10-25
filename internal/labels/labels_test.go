package labels_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/labels"
)

// A repository's labels are read with one gh call however often the picker
// opens, and a label with a space in its name reaches the removal endpoint
// escaped.
//
//nolint:paralleltest // Apply sets PATH for the whole process
func TestReadAsksOncePerRepositoryAndSetEscapesTheName(t *testing.T) {
	const repo = "acme/widgets"

	at := labels.Target{Repo: repo, Number: 7}

	path := filepath.Join(t.TempDir(), "labels.golden")
	if err := ghcassette.Save(path, &ghcassette.Cassette{Interactions: []ghcassette.Interaction{
		{
			Args: labels.ListArgs(repo),
			Stdout: `[{"name":"area: api","color":"0e8a16","description":"the HTTP surface"},` +
				`{"name":"bug","color":"d73a4a"}]`,
		},
		{Args: labels.SetArgs(at, "area: api", false), Stdout: "[]"},
	}}); err != nil {
		t.Fatal(err)
	}

	s := ghcassette.Replay(t, path)
	s.Apply(t)

	for range 2 {
		got, err := labels.Read(t.Context(), ".", repo)
		if err != nil {
			t.Fatal(err)
		}

		if len(got) != 2 || got[0].Name != "area: api" || got[0].Description != "the HTTP surface" {
			t.Errorf("read %+v", got)
		}
	}

	if err := labels.Set(t.Context(), ghrun.GH(), ".", at, "area: api", false); err != nil {
		t.Fatal(err)
	}

	removal := strings.Join(labels.SetArgs(at, "area: api", false), " ")
	if !strings.HasSuffix(removal, "/issues/7/labels/area:%20api") {
		t.Errorf("removal call = %q", removal)
	}

	s.RequireAllPlayed(t)
}
