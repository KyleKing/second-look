package get_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
)

const roundSHA = "3f9a1c0d7e2b48f6a05c9d3e1b7f4a28c6d0e5b9"

const earlierPatch = `diff --git a/internal/vcs/git.go b/internal/vcs/git.go
index 3333333..4444444 100644
--- a/internal/vcs/git.go
+++ b/internal/vcs/git.go
@@ -200,3 +201,3 @@ func Head() string {
 	return head
`

// stubGH puts a gh on PATH that records its arguments and answers with the
// patch on disk, which is the whole of what gh api compare does here.
func stubGH(t *testing.T, record string) {
	t.Helper()

	bin := t.TempDir()

	patchFile := filepath.Join(bin, "patch")
	if err := os.WriteFile(patchFile, []byte(earlierPatch), 0o600); err != nil {
		t.Fatal(err)
	}

	gh := filepath.Join(bin, "gh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GH_RECORD\"\ncat \"$GH_PATCH\"\n"
	//nolint:gosec // the stub gh on PATH has to be executable
	if err := os.WriteFile(gh, []byte(script), 0o750); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GH_RECORD", record)
	t.Setenv("GH_PATCH", patchFile)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A head the pull request moved past keeps no pinned diff: it is rebuilt from
// the forge's compare endpoint and cached like any other fetch, so the second
// ask never reaches the network.
func TestRoundPatchRebuildsAHeadThePullRequestMovedPast(t *testing.T) {
	record := filepath.Join(t.TempDir(), "argv")
	stubGH(t, record)

	store := t.TempDir()
	target := get.Target{Owner: "kyleking", Repo: "second-look", Number: 2, Store: store}

	got, err := get.RoundPatch(t.Context(), target, "main", roundSHA)
	if err != nil {
		t.Fatalf("reading the round's diff: %v", err)
	}

	if string(got) != earlierPatch {
		t.Errorf("the rebuilt diff differs:\n%s", got)
	}

	//nolint:gosec // the path is this test's own temporary file
	argv, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}

	if want := "repos/kyleking/second-look/compare/main..." + roundSHA; !strings.Contains(string(argv), want) {
		t.Errorf("gh was not asked for the base...head compare, got:\n%s", argv)
	}

	cached, err := os.ReadFile(artifact.DiffPath(store, roundSHA))
	if err != nil {
		t.Fatalf("the rebuilt diff was not cached: %v", err)
	}

	if string(cached) != earlierPatch {
		t.Errorf("the cached diff differs from what was fetched")
	}

	t.Setenv("PATH", t.TempDir())

	again, err := get.RoundPatch(t.Context(), target, "main", roundSHA)
	if err != nil {
		t.Fatalf("the cached round asked the forge again: %v", err)
	}

	if string(again) != earlierPatch {
		t.Errorf("the cached read differs from what was fetched")
	}
}
