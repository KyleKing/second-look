package tui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// The rounds a review was read at. Round one carried hunk A as it reads now,
// round two carried A and B, all three in b.go, and everything else arrived
// after both.
const (
	roundOne = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	roundTwo = "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	stepHead = "3333333"
)

func roundStepModel(t *testing.T, h int) *tui.Model {
	t.Helper()

	now := time.Now()
	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: stepHead, Event: artifact.EventComment,
		Rounds: []artifact.Round{
			{SHA: roundOne, Staged: now.Add(-48 * time.Hour)},
			{SHA: roundTwo, Staged: now.Add(-24 * time.Hour)},
			{SHA: stepHead, Staged: now},
		},
	}, headAtThree())

	m.SetRounds(func(_ context.Context, sha string) (*diff.Diff, error) {
		if sha == roundOne {
			return diff.Parse([]byte(stepFile("b.go", stepHunk("A", 10, 10)))), nil
		}

		return diff.Parse([]byte(stepFile("b.go", stepHunk("A", 10, 10), stepHunk("B", 40, 40)))), nil
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: h})

	return m
}

func stepRound(t *testing.T, m *tui.Model, bracket rune) {
	t.Helper()

	pressKey(t, m, bracket)
	pressKey(t, m, 'r')
}

// ]r and [r walk the comparison between rounds the way diffbandit's log dock
// walks commits: what is hidden changes, and the line being read stays under
// the eye. Each step lands on the same line at the same height of the frame,
// at every size a reader might hold the terminal at.
func TestRoundStep_KeepsCursorOnItsLineAndRow(t *testing.T) {
	t.Parallel()

	for _, h := range []int{24, 30, 50} {
		t.Run(fmt.Sprintf("%d rows", h), func(t *testing.T) {
			t.Parallel()

			m := roundStepModel(t, h)
			onto(t, m, "C new")

			row := frameRow(m)

			for _, tc := range []struct {
				bracket rune
				says    string
				hides   []string
			}{
				{']', "since round 1", []string{"A new"}},
				{']', "since round 2", []string{"A new", "B new"}},
				{'[', "since round 1", []string{"A new"}},
				{'[', "showing every hunk again", nil},
				{']', "since round 1", []string{"A new"}},
			} {
				stepRound(t, m, tc.bracket)
				stillOn(t, m, tc.bracket, tc.says, tc.hides, row)
			}
		})
	}
}

// stillOn checks one step: it said where it is, hid what that round carried,
// and left the cursor on C's changed line at the height it had on the frame.
func stillOn(t *testing.T, m *tui.Model, bracket rune, says string, hides []string, row int) {
	t.Helper()

	frame := plain(m.Frame())
	if !strings.Contains(frame, says) {
		t.Fatalf("%cr never said %q:\n%s", bracket, says, frame)
	}

	for _, gone := range hides {
		if strings.Contains(frame, gone) {
			t.Errorf("%cr left %q drawn:\n%s", bracket, gone, frame)
		}
	}

	if got := m.CursorText(); !strings.Contains(got, "C new") {
		t.Fatalf("%cr moved the cursor off its line onto %q:\n%s", bracket, got, frame)
	}

	if got := frameRow(m); got != row {
		t.Errorf("%cr moved the line from row %d of the frame to row %d", bracket, row, got)
	}
}

// The walk has two ends and each says so rather than doing nothing: the newest
// round before the head, and the whole diff, which is older than every round.
func TestRoundStep_SaysWhereTheWalkEnds(t *testing.T) {
	t.Parallel()

	m := roundStepModel(t, 30)

	stepRound(t, m, '[')

	if got := plain(m.Frame()); !strings.Contains(got, "already showing the whole diff") {
		t.Errorf("[r on the whole diff did not say there is nothing older:\n%s", got)
	}

	stepRound(t, m, ']')
	stepRound(t, m, ']')

	if got := plain(m.Frame()); !strings.Contains(got, "since round 2") {
		t.Fatalf("two ]r did not reach the newest round:\n%s", got)
	}

	stepRound(t, m, ']')

	if got := plain(m.Frame()); !strings.Contains(got, "round 2 is the newest before this head") {
		t.Errorf("]r past the newest round did not say so:\n%s", got)
	}
}

// A review read at one head has nothing to step to, and says so.
func TestRoundStep_WithOneRoundSaysThereIsNone(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)

	stepRound(t, m, ']')

	if got := plain(m.Frame()); !strings.Contains(got, "only been read at one head") {
		t.Errorf("]r on a one-round review did not say why nothing moved:\n%s", got)
	}
}

// A line the round being compared already carried is hidden by the step, so
// the cursor cannot stay on it. It lands on the hunk of the same file nearest
// the line, and the status says the line is gone rather than leaving the
// reader to notice the cursor moved.
func TestRoundStep_LandsOnNearestHunkWhenTheLineIsHidden(t *testing.T) {
	t.Parallel()

	m := roundStepModel(t, 30)
	onto(t, m, "B new")

	stepRound(t, m, ']')
	stepRound(t, m, ']')

	frame := plain(m.Frame())
	if !strings.Contains(frame, "b.go:43 is not drawn now; on the nearest hunk") {
		t.Errorf("the step did not say the line went:\n%s", frame)
	}

	if got := m.CursorFile(); got != "b.go" {
		t.Errorf("the cursor left the file for %q:\n%s", got, frame)
	}

	if got := m.CursorText(); !strings.Contains(got, "C one") {
		t.Errorf("the cursor landed on %q rather than hunk C, the nearest one drawn:\n%s", got, frame)
	}
}
