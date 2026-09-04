package brief_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/brief"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
)

const patch = `diff --git a/pkg/read.go b/pkg/read.go
--- a/pkg/read.go
+++ b/pkg/read.go
@@ -1,4 +1,5 @@
 package pkg
 
-func Read() error { return nil }
+func Read(path string) error {
+	return open(path)
+}
`

func review() *artifact.Review {
	return &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "acme", Repo: "widget", Number: 7,
		Comments: []artifact.Comment{{
			ID: "wrap", Path: "pkg/read.go", Line: 4, Side: artifact.SideRight,
			Body: "This drops the error.", Note: "ran the suite",
			Severity: "major", Status: artifact.StatusReady,
		}, {
			ID: "gone", Path: "pkg/read.go", Line: 3, Side: artifact.SideLeft,
			Body: "Why did this go?", Severity: "question", Status: artifact.StatusDraft,
		}},
	}
}

// The marked diff is the change and what is said about it as one thing, with a
// comment on a removed line marked where it was removed rather than beside
// whatever now sits at that number.
func TestDiffMarksEveryComment(t *testing.T) {
	t.Parallel()

	out := brief.Diff(diff.Parse([]byte(patch)), review())

	for _, want := range []string{
		"pkg/read.go",
		"@@ -1,4 +1,5 @@",
		"<<< wrap  major/ready",
		"<<< gone  question/draft",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from:\n%s", want, out)
		}
	}

	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if !strings.Contains(l, "<<< gone") {
			continue
		}

		if !strings.Contains(lines[i-1], "- func Read() error") {
			t.Errorf("the comment on the removed line marks %q", lines[i-1])
		}
	}
}

// Comment is the whole context of one finding, and the note it carries is the
// half that never leaves the laptop, so it is labeled rather than merged in.
func TestCommentCarriesEverythingAroundIt(t *testing.T) {
	t.Parallel()

	out, err := brief.Comment(review(), "wrap", diff.Parse([]byte(patch)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"wrap  major/ready  pkg/read.go RIGHT 4",
		"This drops the error.",
		"NOTE (never posted)",
		"ran the suite",
		">>    4 + \treturn open(path)",
		"    1   package pkg",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from:\n%s", want, out)
		}
	}
}

// A reply is only readable beside what it answers, so the cached thread is
// printed with it and a thread that is no longer cached says so rather than
// leaving the reply looking like it addresses nothing.
func TestCommentShowsTheThreadItAnswers(t *testing.T) {
	t.Parallel()

	r := review()
	r.Comments = append(r.Comments, artifact.Comment{
		ID: "reply", InReplyTo: 91, Body: "Fixed in the next push.",
		Status: artifact.StatusReady,
	})

	open := []threads.Thread{{
		Path: "pkg/read.go", Side: artifact.SideRight, Line: 4,
		Notes: []threads.Note{{ID: 91, Author: "kyleking", Body: "Does this drop the error?"}},
	}}

	out, err := brief.Comment(r, "reply", diff.Parse([]byte(patch)), open, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"IT ANSWERS", "@kyleking", "Does this drop the error?"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from:\n%s", want, out)
		}
	}

	out, err = brief.Comment(r, "reply", diff.Parse([]byte(patch)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "not in the cached threads") {
		t.Errorf("a reply with no cached thread said nothing about it:\n%s", out)
	}
}

func TestCommentRefusesAnIDTheReviewDoesNotCarry(t *testing.T) {
	t.Parallel()

	if _, err := brief.Comment(review(), "nope", diff.Parse([]byte(patch)), nil, 0); err == nil {
		t.Fatal("an unknown id was accepted")
	}
}
