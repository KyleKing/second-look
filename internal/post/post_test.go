package post_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/post"
)

var errBoom = errors.New("boom")

type fakePoster struct {
	endpoints []string
	bodies    [][]byte
	failOn    int
}

func (f *fakePoster) Post(_ context.Context, endpoint string, body []byte) error {
	f.endpoints = append(f.endpoints, endpoint)
	f.bodies = append(f.bodies, body)

	if f.failOn > 0 && len(f.endpoints) == f.failOn {
		return errBoom
	}

	return nil
}

func inlineComment(id, status string) artifact.Comment {
	return artifact.Comment{ID: id, Path: "a.go", Line: 5, Side: artifact.SideRight, Body: "fix this", Status: status}
}

func review(comments ...artifact.Comment) *artifact.Review {
	return &artifact.Review{
		Version:  artifact.SchemaVersion,
		Owner:    "kyleking",
		Repo:     "second-look",
		Number:   42,
		HeadSHA:  "abc123",
		Event:    artifact.EventComment,
		Body:     "looks good",
		Comments: comments,
	}
}

func writeArtifact(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("writing artifact: %v", err)
	}

	return path
}

func TestRun(t *testing.T) {
	t.Parallel()

	reply := artifact.Comment{ID: "2", InReplyTo: 99, Body: "thanks", Status: artifact.StatusReady}

	tests := []struct {
		name          string
		review        *artifact.Review
		failOn        int
		wantErr       string
		wantEndpoints []string
		wantFileGone  bool
	}{
		{
			name:   "happy path posts the review then each reply and removes the artifact",
			review: review(inlineComment("1", artifact.StatusReady), reply),
			wantEndpoints: []string{
				"/repos/kyleking/second-look/pulls/42/reviews",
				"/repos/kyleking/second-look/pulls/42/comments/99/replies",
			},
			wantFileGone: true,
		},
		{
			name:    "a reply failure leaves the artifact on disk and names it",
			review:  review(inlineComment("1", artifact.StatusReady), reply),
			failOn:  2,
			wantErr: "the review posted but a reply did not",
			wantEndpoints: []string{
				"/repos/kyleking/second-look/pulls/42/reviews",
				"/repos/kyleking/second-look/pulls/42/comments/99/replies",
			},
		},
		{
			name:    "a draft comment blocks the post and nothing is sent",
			review:  review(inlineComment("1", artifact.StatusDraft)),
			wantErr: "still draft",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeArtifact(t)
			p := &fakePoster{failOn: tt.failOn}
			var out bytes.Buffer

			err := post.Run(context.Background(), p, path, tt.review, &out)

			checkErr(t, err, tt.wantErr)
			checkEndpoints(t, p.endpoints, tt.wantEndpoints)
			checkArtifact(t, path, tt.wantFileGone)
		})
	}
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()

	if want == "" {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		return
	}

	if err == nil {
		t.Fatal("Run: want error, got nil")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func checkEndpoints(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("endpoints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("endpoints[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func checkArtifact(t *testing.T, path string, wantGone bool) {
	t.Helper()

	_, err := os.Stat(path)
	switch {
	case wantGone && !os.IsNotExist(err):
		t.Fatalf("artifact file still exists after a successful post: err = %v", err)
	case !wantGone && err != nil:
		t.Fatalf("artifact file missing when it should remain: %v", err)
	}
}
