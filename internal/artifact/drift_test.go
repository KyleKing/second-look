package artifact_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

var driftPatch = strings.Join([]string{
	"diff --git a/pkg/read.go b/pkg/read.go",
	"--- a/pkg/read.go",
	"+++ b/pkg/read.go",
	"@@ -1,2 +1,3 @@",
	" package pkg",
	"+func Read() error { return nil }",
	"+func Write() error { return nil }",
	"",
}, "\n")

// Drift is the posting guard asked at read time. Finding out at submit that
// four comments moved under a force-push is finding out too late.
func TestDriftedNamesWhatMoved(t *testing.T) {
	t.Parallel()

	at := func(id string, line int, anchor, status string) artifact.Comment {
		c := artifact.Comment{
			ID: id, Path: "pkg/read.go", Side: artifact.SideRight,
			Line: line, Anchor: anchor, Status: status,
		}
		if status == artifact.StatusSkip {
			c.SkipReason = "declined"
		}

		return c
	}

	comments := []artifact.Comment{
		at("still", 2, "func Read() error { return nil }", artifact.StatusReady),
		at("moved", 3, "func Write() error { return err }", artifact.StatusReady),
		at("gone", 40, "whatever", artifact.StatusReady),
		at("declined", 40, "whatever", artifact.StatusSkip),
	}

	got := artifact.Drifted(comments, diff.Parse([]byte(driftPatch)))

	if got["still"] {
		t.Error("a comment whose line reads the same was called drifted")
	}

	for _, want := range []string{"moved", "gone"} {
		if !got[want] {
			t.Errorf("%s moved and was not reported", want)
		}
	}

	if got["declined"] {
		t.Error("a skipped comment never posts, so its anchor is nothing to warn about")
	}
}
