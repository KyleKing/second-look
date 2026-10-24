package tui_test

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/tui"
)

// The frames worth pinning are the ones a reader looks at longest and the two
// overlays that cover them. Color is stripped: every state carries a glyph as
// well as a color, so the glyph is what a monochrome terminal shows and what a
// golden file can hold without pinning a lipgloss version.
//
// These drive the model rather than a pty, because Frame is deterministic and a
// terminal is not: the pty tests in cmd/second-look cover what only a real
// terminal can show.
func TestFrames(t *testing.T) {
	t.Parallel()

	frames := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"review", nil},
		{"comment", []tea.KeyPressMsg{{Code: tea.KeyTab}}},
		{"help", []tea.KeyPressMsg{{Code: '?', Text: "?"}}},
		{"confirm", []tea.KeyPressMsg{
			{Code: 'm', Text: "m"}, {Code: 'x', Text: "x"}, {Code: 'S', Text: "S"},
		}},
		{"code", []tea.KeyPressMsg{{Code: 'c', Text: "c"}}},
		{"split", []tea.KeyPressMsg{{Code: 'v', Text: "v"}}},
		{"plain", []tea.KeyPressMsg{
			{Code: 'v', Text: "v"}, {Code: 'v', Text: "v"}, {Code: 'v', Text: "v"},
		}},
		{"comments", []tea.KeyPressMsg{{Code: 'c', Text: "c"}, {Code: 'c', Text: "c"}}},
	}

	for _, width := range []int{80, 120} {
		for _, frame := range frames {
			t.Run(fmt.Sprintf("%s/%d", frame.name, width), func(t *testing.T) {
				t.Parallel()

				m := triaged(t)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})

				for _, k := range frame.keys {
					press(t, m, k)
				}

				golden.RequireEqual(t, []byte(plain(m.Frame())))
			})
		}
	}
}

// The review's own prose opens the screen and is the block a real review has
// most of. What is pinned here is that a list wraps under its own text rather
// than under its marker, that an indent survives, and that the note is a block
// of its own rather than more of the body.
func TestReviewProseFrames(t *testing.T) {
	t.Parallel()

	body := "Picking up the review. I checked production and:\n\n" +
		"1. Across 177 live asks, seven orgs carry an id owned by another org, " +
		"which is the case this change is for\n" +
		"2. The org in the ticket has none of it: no cross-org pointer, no duplicate ask\n" +
		"- it does have two live questionnaires sharing a name"
	note := "PROD EVIDENCE (read-only). The orgs named here are real.\n\n" +
		"- 177 live asks across 7 orgs carry an id owned by a different org, and the " +
		"template asks do the same\n" +
		"    - every pair of them matches a fork run, so the fork is the source"

	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("prose/%d", width), func(t *testing.T) {
			t.Parallel()

			m, _, _ := modelFor(t, &artifact.Review{
				Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
				HeadSHA: "a1b2c3d", Event: artifact.EventComment, Body: body, Note: note,
			}, patch)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})

			// The review's own prose starts closed, and how it wraps is what
			// these frames are for.
			go2(t, m, 'z', 'R')

			golden.RequireEqual(t, []byte(plain(m.Frame())))
		})
	}
}

// The plan's views at the three sizes their layout changes across: the tour's
// rail and margin beside the diff at 160 columns and past, and the strip with
// the stop's note inline below that; the outline's detail beside its list, and
// under it.
func TestTourFrames(t *testing.T) {
	t.Parallel()

	const tour = `diff --git a/api/route.go b/api/route.go
--- a/api/route.go
+++ b/api/route.go
@@ -1,2 +1,2 @@
 func route() {
-	serve(1)
+	serve(2)
diff --git a/core/lock.go b/core/lock.go
--- a/core/lock.go
+++ b/core/lock.go
@@ -4,2 +4,3 @@
 func save() {
+	lock()
 	write()
diff --git a/gen/client.go b/gen/client.go
--- a/gen/client.go
+++ b/gen/client.go
@@ -1,2 +1,3 @@
 package gen
+var A = 1
+var B = 2
`

	frames := []struct {
		name string
		keys []rune
	}{{"tour", []rune{'n'}}, {"outline", []rune{'I'}}}

	for _, size := range [][2]int{{200, 60}, {140, 40}, {80, 24}} {
		for _, frame := range frames {
			t.Run(fmt.Sprintf("%s/%dx%d", frame.name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				m, _, _ := modelFor(t, &artifact.Review{
					Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
					HeadSHA: "a1b2c3d", Event: artifact.EventComment,
					Plan: artifact.Plan{Stops: []artifact.PlanStop{{
						ID: "lock", Role: "the core decision", Origin: "claude-code",
						Span: artifact.Span{
							Path: "core/lock.go", Side: artifact.SideRight, Line: 5, Anchor: "\tlock()",
						},
						What: "Every save takes the lock before it writes.",
						Then: "The route waits on it, with no timeout.",
					}}},
				}, tour, tui.WithPlanRules(plan.Rules{Repo: "kyleking/jj-diff", Fold: []string{"gen/**"}}))
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range frame.keys {
					pressKey(t, m, k)
				}

				golden.RequireEqual(t, []byte(plain(m.Frame())))
			})
		}
	}
}

// triaged is a review in every state the screen can show: one comment ready to
// post, one still a draft, and one skipped with its reason.
func triaged(t *testing.T) *tui.Model {
	t.Helper()

	ready := comment("ready", parsed, artifact.SideRight, 15, "split can fail now, so this has to check err.")

	draft := comment("draft", parsed, artifact.SideRight, 14, "Is first still 1 after the change?")
	draft.Status = artifact.StatusDraft
	draft.Severity = "question"
	draft.Note = "Unverified: I have not read the caller."

	skipped := comment("skipped", "internal/vcs/git.go", artifact.SideRight, 201, "Rename this to headSHA.")
	skipped.Status = artifact.StatusSkip
	skipped.Severity = "nit"
	skipped.SkipReason = "Out of scope for this change."

	m, _ := fixture(t, ready, draft, skipped)

	return m
}
