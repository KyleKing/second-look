package artifact_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
)

// stagedAt is the diff the plan was staged against, and movedHead the same change
// after a push that put a hunk above it and changed its middle line.
const (
	stagedAt = `diff --git a/internal/one.go b/internal/one.go
index 1111111..2222222 100644
--- a/internal/one.go
+++ b/internal/one.go
@@ -10,3 +10,3 @@ func one() {
 	first := 1
-	dropped := 2
+	added := 2
 	last := 3
`
	movedHead = `diff --git a/internal/one.go b/internal/one.go
index 1111111..3333333 100644
--- a/internal/one.go
+++ b/internal/one.go
@@ -1,1 +1,2 @@
 package one
+// one does one thing.
@@ -14,3 +15,3 @@ func one() {
 	first := 1
-	dropped := 2
+	renamed := 2
 	last := 3
`
)

func right(line int) artifact.Span {
	return artifact.Span{Path: "internal/one.go", Side: artifact.SideRight, Line: line}
}

// The plan is anchored the way a comment is, so a head that moved carries each
// entry to where its line went, and one whose line changed is reported.
func TestPlanFollowsTheHeadItWasStagedAgainst(t *testing.T) {
	t.Parallel()

	ask := right(12)
	ask.StartLine = 10

	p := artifact.Plan{
		Claim: "one adds a thing",
		Stops: []artifact.PlanStop{
			{ID: "first", Span: right(10), What: "where it starts", Origin: "claude"},
			{ID: "second", Span: right(11), What: "the line that changed", Origin: artifact.OriginMe},
		},
		Chapters: []artifact.PlanChapter{{
			Chapter: "internal", Name: "the change", Mode: plan.ModeSkim, Hunks: []artifact.Span{right(10)},
		}},
		Asks: []artifact.PlanAsk{{ID: "q", Span: ask, Question: "why two?"}},
	}

	if err := artifact.ResolvePlan(&p, diff.Parse([]byte(stagedAt))); err != nil {
		t.Fatal(err)
	}

	got := p.Overlay(diff.Parse([]byte(movedHead)))

	moved := order.Ref{Path: "internal/one.go", Hunk: 2}

	if len(got.Stops) != 1 || got.Stops[0].Ref != moved || got.Stops[0].Source != plan.SourceAgent {
		t.Errorf("the stops came back as %+v", got.Stops)
	}

	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "stop second") {
		t.Errorf("the changed line is not the one problem: %q", got.Problems)
	}

	if c := got.Chapters[0]; len(c.Hunks) != 1 || c.Hunks[0] != moved || c.Mode != plan.ModeSkim {
		t.Errorf("the chapter edit came back as %+v", c)
	}

	if len(got.Asks) != 1 || got.Asks[0].StartLine != 15 || got.Asks[0].Line != 17 {
		t.Errorf("the ask did not move with its lines: %+v", got.Asks)
	}
}

func TestPlanIsCheckedAndKept(t *testing.T) {
	t.Parallel()

	r := artifact.Review{
		Version: artifact.SchemaVersion, Owner: "o", Repo: "r", Number: 1,
		Plan: artifact.Plan{Stops: []artifact.PlanStop{{ID: "s", Span: right(11), What: "here"}}},
	}

	path := filepath.Join(t.TempDir(), "pr-1.toml")
	if err := artifact.Save(path, &r); err != nil {
		t.Fatal(err)
	}

	back, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(back.Plan.Stops) != 1 || back.Plan.Stops[0].Span != right(11) {
		t.Errorf("the plan came back as %+v", back.Plan)
	}

	r.Plan = artifact.Plan{
		Stops:    []artifact.PlanStop{{Span: artifact.Span{Path: "a", Side: "UP", Line: 1}}},
		Chapters: []artifact.PlanChapter{{Mode: "squint"}},
		Asks:     []artifact.PlanAsk{{ID: "a", Span: right(1)}, {ID: "a", Span: right(1), Question: "?"}},
	}

	err = r.Validate()
	for _, want := range []error{
		artifact.ErrNoID, artifact.ErrNoWhat, artifact.ErrSide, artifact.ErrNoChapter, plan.ErrMode,
		artifact.ErrNoAsk, artifact.ErrDuplicateID,
	} {
		if !errors.Is(err, want) {
			t.Errorf("%v is not reported in %v", want, err)
		}
	}

	if err := artifact.ResolvePlan(&artifact.Plan{Stops: []artifact.PlanStop{{Span: right(993)}}},
		diff.Parse([]byte(patch))); !errors.Is(err, artifact.ErrAnchorMissing) {
		t.Errorf("a stop off the diff answered %v", err)
	}
}
