package tui_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/seen"
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

// The plan's views at the sizes their layout changes across: beside the diff or
// the list from 160 columns, and inline or stacked below that.
func TestTourFrames(t *testing.T) {
	t.Parallel()

	outline := tea.KeyPressMsg{Code: 'I', Text: "I"}
	frames := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"tour", []tea.KeyPressMsg{{Code: 'n', Text: "n"}}},
		{"outline", []tea.KeyPressMsg{outline}},
		{"features", []tea.KeyPressMsg{outline, {Code: tea.KeyTab}}},
	}

	for _, size := range [][2]int{{200, 60}, {140, 40}, {80, 24}} {
		for _, frame := range frames {
			t.Run(fmt.Sprintf("%s/%dx%d", frame.name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				m := toured(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range frame.keys {
					press(t, m, k)
				}

				golden.RequireEqual(t, []byte(plain(m.Frame())))
			})
		}
	}
}

// A stop's heading draws only on the tour, so p and then c from one land on its
// hunk rather than at the top.
func TestPlainFromAStopHeadingKeepsTheCursorOnItsHunk(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{200, 60}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			m := toured(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			pressKey(t, m, 'n')

			if got := m.CursorText(); !strings.Contains(got, "stop 1 of 1") {
				t.Fatalf("n landed on %q, want the stop's heading", got)
			}

			for _, k := range []rune{'p', 'c'} {
				pressKey(t, m, k)

				if got := headerLine(m); m.CursorRow() == 0 || !strings.Contains(got, "lock.go") {
					t.Errorf("%c from the stop's heading left the cursor on row %d of %q, want the stop's hunk",
						k, m.CursorRow(), got)
				}
			}
		})
	}
}

// toured is a plan with a stop, a rule's fold, and two features sharing a file,
// which is everything the tour, the outline, and the features draw.
func toured(t *testing.T) *tui.Model {
	t.Helper()

	const tour = `diff --git a/api/route.go b/api/route.go
--- a/api/route.go
+++ b/api/route.go
@@ -1,2 +1,2 @@
 func route() {
-	serve(1)
+	serve(2)
@@ -10,2 +10,3 @@
 func call() {
+	waitLock()
 	done()
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
		}}, Chapters: []artifact.PlanChapter{
			{Chapter: "api", Feature: "routing", Origin: "claude-code"},
			{Chapter: "core", Feature: "locking", Origin: "claude-code"},
			{
				Chapter: "the lock's caller", Feature: "locking", Origin: "claude-code",
				Hunks: []artifact.Span{{
					Path: "api/route.go", Side: artifact.SideRight, Line: 11, Anchor: "\twaitLock()",
				}},
			},
		}},
	}, tour, tui.WithPlanRules(plan.Rules{Repo: "kyleking/jj-diff", Fold: []string{"gen/**"}}))

	return m
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

// shaped is six cards making one edit, two doing more beside it, and a search
// of the head that finds one card left out and one member it must not list.
func shaped(t *testing.T) *tui.Model {
	t.Helper()

	var b strings.Builder

	for i, name := range []string{"CardA", "CardB", "CardC", "CardD", "CardE", "CardF"} {
		extra, size := "", 2
		if i >= 4 {
			extra, size = fmt.Sprintf("+  const limit = %d\n", i), 3
		}

		fmt.Fprintf(&b, "diff --git a/cards/%[1]s.tsx b/cards/%[1]s.tsx\n"+
			"--- a/cards/%[1]s.tsx\n+++ b/cards/%[1]s.tsx\n@@ -3,2 +3,%[2]d @@ export function %[1]s() {\n"+
			"-  const id = useSuspenseVendor(data => data.id)\n+  const id = useVendorId()\n%[3]s"+
			"   return <%[1]sBody id={id} />\n", name, size, extra)
	}

	b.WriteString("diff --git a/boot.go b/boot.go\n--- a/boot.go\n+++ b/boot.go\n" +
		"@@ -1,1 +1,2 @@\n func boot() {\n+\twarm(cache)\n")

	search := func(_ context.Context, text string) ([]blob.Hit, error) {
		if text != "const id = useSuspenseVendor(data => data.id)" {
			return nil, nil
		}

		return []blob.Hit{{Path: "cards/CardA.tsx", Line: 3}, {Path: "cards/RisksCard.tsx", Line: 4}}, nil
	}

	read := func(context.Context, string) ([]string, error) {
		return []string{"export function RisksCard() {", "", "", "  const id = useSuspenseVendor(data => data.id)"}, nil
	}

	at := filepath.Join(t.TempDir(), "seen.toml")

	marks, err := seen.Load(at)
	if err != nil {
		t.Fatal(err)
	}

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}, b.String(), tui.WithHeadSearch(search), tui.WithBlobs(read), tui.WithSeen(marks, at))
	m.SearchHead()

	return m
}

// A shape chapter draws its edit once, a row per card saying what it does
// beyond it, and the files at head the edit missed; space on a card's row
// marks its hunks read, and enter on a missed file opens it.
func TestAShapeChapterReadsTheEditOnce(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{200, 60}, {140, 40}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			m := shaped(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			go2(t, m, ']', ']')

			golden.RequireEqual(t, []byte(plain(m.Frame())))
		})
	}

	m := shaped(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})

	for _, k := range "/CardE.tsx" {
		pressKey(t, m, k)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	pressKey(t, m, ' ')

	if got := plain(m.Frame()); !strings.Contains(got, "✓ CardE.tsx") {
		t.Errorf("space on a card's row did not mark it read:\n%s", got)
	}

	for _, k := range "/RisksCard" {
		pressKey(t, m, k)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := plain(m.Frame()); !strings.Contains(got, "RisksCard.tsx:4 · file · outside the diff") {
		t.Errorf("enter on a file the edit missed did not open it:\n%s", got)
	}
}
