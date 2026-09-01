package resolve_test

import (
	"path/filepath"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/resolve"
)

// The thread on [KyleKing/second-look#2] the recording was made against, and the
// comment that opened it. Both are real ids: the recording sent the resolve and
// the thumbs-up to GitHub, so what replays here is the request GitHub accepted
// rather than a guess at its shape.
//
// [KyleKing/second-look#2]: https://github.com/KyleKing/second-look/pull/2
const (
	liveThread  = "PRRT_kwDOT-RSFs6d8L5o"
	liveComment = "PRRC_kwDOT-RSFs7odd7g"
)

// TestRunAgainstGitHub is the one test that has sent these two mutations for
// real. Everything else here drives a fake runner, which proves which calls are
// made and nothing about whether GitHub accepts them.
//
// Re-recording needs a thread that is still unresolved and a comment with no
// thumbs-up from you: this one is resolved now, and addReaction refuses a
// duplicate, so the same pair cannot be recorded twice. Open a thread on #2,
// put its two ids above, and run
//
//	GHCASSETTE_RECORD=1 go test ./internal/resolve/
//
// Apply points this process's own gh at the cassette, which rules out
// t.Parallel: it works by setting PATH for the whole process.
//
//nolint:paralleltest // Apply sets PATH for the whole process
func TestRunAgainstGitHub(t *testing.T) {
	s := ghcassette.Start(t, filepath.Join("testdata", "cassettes", "resolve.golden"))
	s.Apply(t)

	c := &conversations.Conversation{
		Kind: conversations.KindThread, Repository: "KyleKing/second-look", Number: 2,
		ThreadID: liveThread, Path: "testdata/fixture/sample.go", Line: 24,
		Notes: []conversations.Note{{NodeID: liveComment, Author: "KyleKing", Body: "a finding"}},
	}

	status, err := resolve.Run(t.Context(), ghrun.GH(), ".", c)
	if err != nil {
		t.Fatal(err)
	}

	if want := "resolved and thumbs-upped KyleKing/second-look#2 testdata/fixture/sample.go:24"; status != want {
		t.Errorf("status = %q, want %q", status, want)
	}

	s.RequireAllPlayed(t)
}
