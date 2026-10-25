package people_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/people"
)

// The repository's recent logins come from one call per day however many
// reviews ask, bots keep the login GitHub gives them, and a deleted account
// (no login) is skipped.
//
//nolint:paralleltest // Apply sets PATH for the whole process
func TestRosterReadsThePullRequestAndTheRepository(t *testing.T) {
	const repo = "acme/widgets"

	path := filepath.Join(t.TempDir(), "people.golden")
	if err := ghcassette.Save(path, &ghcassette.Cassette{Interactions: []ghcassette.Interaction{
		{
			Args: people.RecentArgs(repo),
			Stdout: `{"data":{"repository":{"pullRequests":{"nodes":[` +
				`{"author":{"login":"ada"},"comments":{"nodes":[{"author":{"login":"watch-doggo"}},{"author":{}}]},` +
				`"reviews":{"nodes":[{"author":{"login":"grace"}}]}},` +
				`{"author":{"login":"ada"},"comments":{"nodes":[]},` +
				`"reviews":{"nodes":[{"author":{"login":"watch-doggo"}}]}}]}}}}`,
		},
		{
			Args: people.InvolvedArgs(repo, 7),
			Stdout: `{"data":{"repository":{"pullRequest":{"author":{"login":"ada"},` +
				`"participants":{"nodes":[{"login":"ada"},{"login":"linus"}]},` +
				`"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"watch-doggo"}},` +
				`{"requestedReviewer":{}}]}}}}}`,
		},
	}}); err != nil {
		t.Fatal(err)
	}

	s := ghcassette.Replay(t, path)
	s.Apply(t)

	for range 2 {
		recent, err := people.Recent(t.Context(), ".", repo)
		if err != nil {
			t.Fatal(err)
		}

		if want := []string{"ada", "grace", "watch-doggo"}; !slices.Equal(recent, want) {
			t.Errorf("recent = %v, want %v", recent, want)
		}
	}

	involved, err := people.Involved(t.Context(), ".", repo, 7)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"ada", "linus", "watch-doggo"}; !slices.Equal(involved, want) {
		t.Errorf("involved = %v, want %v", involved, want)
	}

	s.RequireAllPlayed(t)
}
