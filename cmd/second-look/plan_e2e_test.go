package main_test

import (
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/plan"
)

// An agent shapes the reading plan through the CLI the way it stages comments:
// each verb lands in the local [plan] table anchored to the diff, and a line the
// diff does not carry changes nothing.
func TestPlanVerbsStageTheReadingPlan(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	seedReview(t, dir, sha)
	seedDiffAt(t, dir, sha)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "plan-verbs", func(c *ghcassette.Cassette) {
		inCheckout(c)
		c.Interactions = nil
	}))

	stagePlan(t, s, dir)

	path := artifact.Path(stored(t, dir), 2)

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	checkPlan(t, r.Plan)

	off := `{"stops":[{"id":"invented",` + onFixture + `,"line":993,"what":"nowhere"}]}`
	if res := runCLIStdin(t, s, dir, off, "plan", "stop", "2"); res.code == 0 {
		t.Error("a stop on a line the diff does not carry was staged")
	}

	if again, err := artifact.Load(path); err != nil || len(again.Plan.Stops) != 1 {
		t.Errorf("a refused batch changed the plan: %v %+v", err, again.Plan.Stops)
	}

	if res := runCLI(t, s, dir, "plan"); res.code != 0 || !strings.Contains(res.stdout, "second-look plan stop <pr>") {
		t.Errorf("bare plan printed %q", res.stdout+res.stderr)
	}

	s.RequireAllPlayed(t)
}

const onFixture = `"path":"testdata/fixture/sample.go","side":"RIGHT"`

func stagePlan(t *testing.T, s *ghcassette.Session, dir string) {
	t.Helper()

	for _, step := range []struct{ verb, batch string }{
		{"claim", `{"claim":"read a budget file and total it"}`},
		{"stop", `{"stops":[{"id":"entry",` + onFixture + `,"line":16,"what":"the entry point","then":"splitLines"}]}`},
		{"chapter", `{"chapters":[{"chapter":"testdata/fixture","name":"the reader",` +
			`"hunks":[{` + onFixture + `,"line":19}]}]}`},
		{"mode", `{"chapter":"testdata/fixture","mode":"skim"}`},
		{"ask", `{"asks":[{"id":"q1",` + onFixture + `,"start_line":17,"line":20,"question":"why not wrap?"}]}`},
	} {
		if res := runCLIStdin(t, s, dir, step.batch, "plan", step.verb, "2"); res.code != 0 {
			t.Fatalf("plan %s failed: %s%s", step.verb, res.stdout, res.stderr)
		}
	}
}

func checkPlan(t *testing.T, p artifact.Plan) {
	t.Helper()

	if p.Claim != "read a budget file and total it" || len(p.Stops) != 1 || len(p.Asks) != 1 {
		t.Fatalf("the plan came back as %+v", p)
	}

	if p.Stops[0].Anchor != "func ReadBudget(path string) (int, error) {" || p.Stops[0].Origin != "agent" {
		t.Errorf("the stop is anchored to %q by %q", p.Stops[0].Anchor, p.Stops[0].Origin)
	}

	if len(p.Chapters) != 1 {
		t.Fatalf("mode added a chapter edit rather than changing one: %+v", p.Chapters)
	}

	if c := p.Chapters[0]; c.Name != "the reader" || c.Mode != plan.ModeSkim || len(c.Hunks) != 1 {
		t.Errorf("mode lost the chapter it edits: %+v", c)
	}
}
