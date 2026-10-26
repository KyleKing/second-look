package prstate_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/prstate"
)

// answerFor is the reply to one batched query, in the shape gh prints: an
// even-numbered pull request merged and an odd one is open, so a state landing
// on the wrong ref is visible.
func answerFor(refs []prstate.Ref) string {
	parts := make([]string, 0, len(refs))

	for i, r := range refs {
		state := "OPEN"
		if r.Number%2 == 0 {
			state = "MERGED"
		}

		parts = append(parts, fmt.Sprintf(
			`"r%d":{"pullRequest":{"state":%q,"reviewDecision":"","viewerLatestReview":null}}`, i, state))
	}

	return `{"data":{` + strings.Join(parts, ",") + `}}`
}

func cassetteFor(t *testing.T, refs []prstate.Ref) string {
	t.Helper()

	cassette := &ghcassette.Cassette{}

	for chunk := range slices.Chunk(refs, prstate.MaxAliases) {
		args, err := prstate.BatchArgs(chunk)
		if err != nil {
			t.Fatal(err)
		}

		cassette.Interactions = append(cassette.Interactions,
			ghcassette.Interaction{Args: args, Stdout: answerFor(chunk)})
	}

	path := filepath.Join(t.TempDir(), "batch.golden")
	if err := ghcassette.Save(path, cassette); err != nil {
		t.Fatal(err)
	}

	return path
}

// Reading the state of every staged review is one call per MaxAliases reviews,
// not one per review, and every answer lands on the review it was asked about.
// Replaying in-process sets the environment, which rules out t.Parallel.
//
//nolint:paralleltest // ghcassette.Apply sets the process environment
func TestFetchAllBatchesTheQueryAndKeepsEachAnswerOnItsRef(t *testing.T) {
	for _, c := range []struct{ refs, calls int }{
		{45, 1}, {prstate.MaxAliases, 1}, {prstate.MaxAliases + 1, 2}, {2*prstate.MaxAliases + 5, 3},
	} {
		t.Run(fmt.Sprintf("%d reviews", c.refs), func(t *testing.T) {
			refs := make([]prstate.Ref, 0, c.refs)
			for i := range c.refs {
				refs = append(refs, prstate.Ref{Repository: "example/widgets", Number: i + 1})
			}

			s := ghcassette.Replay(t, cassetteFor(t, refs))
			s.Apply(t)

			got, err := prstate.FetchAll(context.Background(), t.TempDir(), refs)
			if err != nil || len(got) != c.refs {
				t.Fatalf("answered %d of %d reviews: %v", len(got), c.refs, err)
			}

			for _, r := range refs {
				if got[r].Merged() != (r.Number%2 == 0) {
					t.Errorf("#%d came back with another review's state", r.Number)
				}
			}

			if played := len(s.Played(t)); played != c.calls {
				t.Errorf("made %d gh calls, want %d", played, c.calls)
			}
		})
	}
}
