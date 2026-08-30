package artifact_test

import (
	"errors"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

const patch = `diff --git a/internal/one.go b/internal/one.go
index 1111111..2222222 100644
--- a/internal/one.go
+++ b/internal/one.go
@@ -10,3 +10,3 @@ func one() {
 	first := 1
-	dropped := 2
+	added := 2
`

func TestResolve(t *testing.T) {
	t.Parallel()

	comments := []artifact.Comment{
		{ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 11, Status: artifact.StatusReady},
		{ID: "b", Path: "internal/one.go", Side: artifact.SideLeft, Line: 11, Status: artifact.StatusReady},
		{ID: "reply", InReplyTo: 7, Status: artifact.StatusReady},
	}

	if err := artifact.Resolve(comments, diff.Parse([]byte(patch))); err != nil {
		t.Fatal(err)
	}

	if comments[0].Anchor != "\tadded := 2" {
		t.Errorf("right anchor = %q", comments[0].Anchor)
	}
	if comments[1].Anchor != "\tdropped := 2" {
		t.Errorf("left anchor = %q", comments[1].Anchor)
	}
	if comments[2].Anchor != "" {
		t.Errorf("a reply took an anchor: %q", comments[2].Anchor)
	}
}

// A bot citing a line that is not in the diff is the failure the guard exists
// for, so it is rejected while staging rather than on the wire.
func TestResolve_RejectsALineTheDiffDoesNotCarry(t *testing.T) {
	t.Parallel()

	comments := []artifact.Comment{
		{ID: "invented", Path: "internal/one.go", Side: artifact.SideRight, Line: 993, Status: artifact.StatusReady},
	}

	err := artifact.Resolve(comments, diff.Parse([]byte(patch)))
	if !errors.Is(err, artifact.ErrAnchorMissing) {
		t.Fatalf("err = %v, want ErrAnchorMissing", err)
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		comment artifact.Comment
		want    error
	}{
		{
			name: "the line still reads the same",
			comment: artifact.Comment{
				ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 11,
				Anchor: "\tadded := 2", Status: artifact.StatusReady,
			},
		},
		{
			name: "the line moved under the comment",
			comment: artifact.Comment{
				ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 11,
				Anchor: "\tsomething else", Status: artifact.StatusReady,
			},
			want: artifact.ErrAnchorMoved,
		},
		{
			name: "the line left the diff",
			comment: artifact.Comment{
				ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 40,
				Anchor: "\tadded := 2", Status: artifact.StatusReady,
			},
			want: artifact.ErrAnchorMissing,
		},
		{
			name: "a comment staged before the guard existed",
			comment: artifact.Comment{
				ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 11,
				Status: artifact.StatusReady,
			},
			want: artifact.ErrNoAnchor,
		},
		{
			name: "a skipped comment never posts, so it is never checked",
			comment: artifact.Comment{
				ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 993,
				Status: artifact.StatusSkip, SkipReason: "declined",
			},
		},
	}

	d := diff.Parse([]byte(patch))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := artifact.Verify([]artifact.Comment{tc.comment}, d)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDiffCache_RoundTripAndSHAGuard(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sha := "0123456789abcdef0123456789abcdef01234567"

	if err := artifact.SaveDiff(root, sha, []byte(patch)); err != nil {
		t.Fatal(err)
	}

	got, err := artifact.LoadDiff(root, sha)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != patch {
		t.Errorf("the cached diff came back changed")
	}

	// The sha reaches the filesystem as a path, so anything that is not a plain
	// object name is refused rather than joined.
	if err := artifact.SaveDiff(root, "../escape", nil); !errors.Is(err, artifact.ErrNotASHA) {
		t.Errorf("err = %v, want ErrNotASHA", err)
	}
	if _, err := artifact.LoadDiff(root, ""); !errors.Is(err, artifact.ErrNoHeadSHA) {
		t.Errorf("err = %v, want ErrNoHeadSHA", err)
	}
}

// seriesPatch carries internal/one.go twice, which is what a per-commit patch
// series looks like and what the line numbers cannot be trusted from.
const seriesPatch = patch + `diff --git a/internal/one.go b/internal/one.go
index 2222222..3333333 100644
--- a/internal/one.go
+++ b/internal/one.go
@@ -8,2 +8,3 @@ func one() {
 	zeroth := 0
+	inserted := 1
`

func TestAnchorGuard_RefusesAPatchSeries(t *testing.T) {
	t.Parallel()

	comments := []artifact.Comment{
		{
			ID: "a", Path: "internal/one.go", Side: artifact.SideRight, Line: 11,
			Anchor: "\tadded := 2", Status: artifact.StatusReady,
		},
	}

	d := diff.Parse([]byte(seriesPatch))

	if err := artifact.Resolve(comments, d); !errors.Is(err, artifact.ErrNotACumulativeDiff) {
		t.Errorf("Resolve() = %v, want ErrNotACumulativeDiff", err)
	}
	if err := artifact.Verify(comments, d); !errors.Is(err, artifact.ErrNotACumulativeDiff) {
		t.Errorf("Verify() = %v, want ErrNotACumulativeDiff", err)
	}
}
