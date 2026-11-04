package post_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/post"
)

// stagedPNG is a file the test's root can be asked for; the bytes only have to
// exist, since the fake upload never inspects them.
func stagedPNG(t *testing.T, root, name string) {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("making the screenshot's directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatalf("writing the screenshot: %v", err)
	}
}

func TestRunExpandsAttachRefs(t *testing.T) {
	t.Parallel()

	path := writeArtifact(t)
	root := filepath.Dir(filepath.Dir(path))
	stagedPNG(t, root, "shots/ui.png")

	r := review(
		artifact.Comment{
			ID: "c1", Path: "a.go", Line: 5, Side: artifact.SideRight,
			Body: "this is broken ![the button](attach:shots/ui.png) see above", Status: artifact.StatusReady,
		},
	)
	r.Body = "overall ![](attach:shots/ui.png)"

	p := &fakePoster{}
	var out bytes.Buffer

	if err := post.Run(context.Background(), p, path, r, &out); err != nil {
		t.Fatalf("posting: %v", err)
	}

	joined := string(bytes.Join(p.bodies, nil))
	if strings.Contains(joined, "attach:") {
		t.Errorf("an attach: path reached the payload: %s", joined)
	}
	if !strings.Contains(joined, "https://github.com/user-attachments/assets/fake-ui.png") {
		t.Errorf("the uploaded URL did not reach the payload: %s", joined)
	}
	if !strings.Contains(joined, "the button") {
		t.Errorf("the alt text was lost: %s", joined)
	}

	uploads := 0
	for _, e := range p.endpoints {
		if strings.HasPrefix(e, "upload ") {
			uploads++
		}
	}
	if uploads != 1 {
		t.Errorf("the same file was uploaded %d times, want 1", uploads)
	}
}

func TestRunWithoutAttachCostsNoLookup(t *testing.T) {
	t.Parallel()

	path := writeArtifact(t)
	p := &fakePoster{}
	var out bytes.Buffer

	r := review(inlineComment("c1", artifact.StatusReady))
	if err := post.Run(context.Background(), p, path, r, &out); err != nil {
		t.Fatalf("posting: %v", err)
	}

	for _, e := range p.endpoints {
		if strings.HasPrefix(e, "upload ") || e == "graphql repoID" {
			t.Errorf("a body without attach: still spent a call: %s", e)
		}
	}
}

func TestRunExpandsBracketedAttach(t *testing.T) {
	t.Parallel()

	path := writeArtifact(t)
	root := filepath.Dir(filepath.Dir(path))
	stagedPNG(t, root, "shots/Screenshot 2026-11-04.png")

	r := review(artifact.Comment{
		ID: "c1", Path: "a.go", Line: 5, Side: artifact.SideRight,
		Body: "broken ![](attach:<shots/Screenshot 2026-11-04.png>)", Status: artifact.StatusReady,
	})

	p := &fakePoster{}
	var out bytes.Buffer

	if err := post.Run(context.Background(), p, path, r, &out); err != nil {
		t.Fatalf("posting: %v", err)
	}

	joined := string(bytes.Join(p.bodies, nil))
	if !strings.Contains(joined, "fake-Screenshot 2026-11-04.png") {
		t.Errorf("the bracketed path did not upload: %s", joined)
	}
}

func TestAttachRefusals(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		body string
		want string
	}{
		"missing file":       {"![x](attach:shots/gone.png)", "attach:shots/gone.png"},
		"not an image":       {"![x](attach:notes.txt)", "not an image"},
		"spaces unbracketed": {"![x](attach:my file.png)", "attach:<path>"},
		"empty path":         {"![x](attach:)", "attach:<path>"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := review(artifact.Comment{
				ID: "c1", Path: "a.go", Line: 5,
				Side: artifact.SideRight, Body: tt.body, Status: artifact.StatusReady,
			})

			err := post.Run(context.Background(), &fakePoster{}, writeArtifact(t), r, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("wanted a refusal naming %q, got %v", tt.want, err)
			}
		})
	}
}
