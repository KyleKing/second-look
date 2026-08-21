package artifact_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
)

func review(cs ...artifact.Comment) *artifact.Review {
	return &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: cs,
	}
}

func ready(id string) artifact.Comment {
	return artifact.Comment{
		ID: id, Path: "internal/vcs/diff.go", Line: 16, Side: artifact.SideRight,
		Body: "posted text", Status: artifact.StatusReady,
	}
}

// The whole point of the split: a private field must be unreachable from the
// payload, not merely omitted by a caller who remembered.
func TestLocalFieldsNeverReachThePayload(t *testing.T) {
	t.Parallel()

	secret := "SHOULD-NEVER-POST"
	c := ready("c1")
	c.Note, c.Severity, c.SkipReason = secret, "blocker", secret

	r := review(c)
	r.Note = secret

	payload, replies, err := r.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 0 {
		t.Fatalf("replies = %d, want 0", len(replies))
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Errorf("a local field reached the payload: %s", encoded)
	}
	if !strings.Contains(string(encoded), "posted text") {
		t.Errorf("the comment body did not reach the payload: %s", encoded)
	}
}

func TestSkippedCommentsAreNotPosted(t *testing.T) {
	t.Parallel()

	skipped := artifact.Comment{
		ID: "c2", Path: "a.go", Line: 1, Side: artifact.SideRight,
		Body: "considered", Status: artifact.StatusSkip, SkipReason: "style only",
	}

	payload, _, err := review(ready("c1"), skipped).Payload()
	if err != nil {
		t.Fatal(err)
	}

	encoded := mustJSON(t, payload)
	if strings.Contains(encoded, "considered") {
		t.Errorf("a skipped comment was posted: %s", encoded)
	}
}

func TestADraftBlocksPosting(t *testing.T) {
	t.Parallel()

	draft := ready("c2")
	draft.Status = artifact.StatusDraft

	_, _, err := review(ready("c1"), draft).Payload()

	var drafts *artifact.DraftError
	if !errors.As(err, &drafts) {
		t.Fatalf("err = %v, want DraftError", err)
	}
	if len(drafts.Comments) != 1 || drafts.Comments[0].ID != "c2" {
		t.Errorf("drafts = %+v, want just c2", drafts.Comments)
	}
}

func TestRepliesGoToTheirOwnEndpoint(t *testing.T) {
	t.Parallel()

	reply := artifact.Comment{
		ID: "c2", Body: "fixed in the next commit",
		Status: artifact.StatusReady, InReplyTo: 998877,
	}

	payload, replies, err := review(ready("c1"), reply).Payload()
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].InReplyTo != 998877 {
		t.Fatalf("replies = %+v, want one for 998877", replies)
	}

	encoded := mustJSON(t, payload)
	if strings.Contains(encoded, "fixed in the next commit") {
		t.Errorf("a reply was folded into the review payload: %s", encoded)
	}
}

func TestRoundTripPreservesLocalFields(t *testing.T) {
	t.Parallel()

	c := ready("c1")
	c.Note, c.Severity = "go test ./vcs -run Parse => FAIL", "major"

	r := review(c)
	r.Note = "ran the suite on a1b2c3d; the pulumi path is unverified"

	path := filepath.Join(t.TempDir(), artifact.Dir, "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	got, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != r.Note {
		t.Errorf("top-level note = %q, want %q", got.Note, r.Note)
	}
	if got.Comments[0].Note != c.Note || got.Comments[0].Severity != "major" {
		t.Errorf("comment locals = %+v, want note and severity preserved", got.Comments[0])
	}
}

// A misspelled key is a hand-edit that will not do what its author meant, and a
// key the schema does not know is one the posting allowlist cannot classify.
func TestUnknownKeysAreRejected(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, review(ready("c1"))); err != nil {
		t.Fatal(err)
	}

	appendLine(t, path, "\nsecretly_post_this = \"nope\"\n")

	if _, err := artifact.Load(path); err == nil {
		t.Fatal("an unknown key loaded without error")
	} else if !strings.Contains(err.Error(), "secretly_post_this") {
		t.Errorf("err = %v, want it to name the unknown key", err)
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()

	r := review(artifact.Comment{ID: "c1", Body: "x", Status: "whenever", Severity: "extreme"})

	err := r.Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"status", "severity", "path", "line", "side"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err is missing %q: %v", want, err)
		}
	}
}
