package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

// ctrl+n completes from what is already on this screen: the files the diff
// touches and the people who have said something on the pull request. Nothing
// here needs an index.
func TestCompletionOffersWhatTheReviewAlreadyKnows(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	press(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})

	for _, r := range "see diff.g" {
		press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	press(t, m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "see diff.go") {
		t.Errorf("ctrl+n did not complete the file name:\n%s", frame)
	}

	// A stem with nothing behind it says so rather than doing nothing.
	for _, r := range " zzzz" {
		press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	press(t, m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	if frame = plain(m.Frame()); !strings.Contains(frame, "nothing in this review starts with") {
		t.Errorf("a stem matching nothing said:\n%s", frame)
	}
}

// @ completes the people on the pull request first, then whoever the repository
// has seen lately, bots included, and the roster is read when the first @ is
// typed rather than when the review opens.
func TestMentionRanksThePullRequestsPeopleFirst(t *testing.T) {
	t.Parallel()

	reads := 0
	roster := func(context.Context) ([]string, []string, error) {
		reads++

		return []string{"zoe"}, []string{"aaron", "watch-doggo", "zoe"}, nil
	}

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}, patch, tui.WithAbout(threads.About{Author: "kyleking"}),
		tui.WithThreads([]threads.Thread{finding("a", 15, "doggo", "wrap it")}), tui.WithRoster(roster))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if reads != 0 {
		t.Fatalf("the roster was read %d times before anyone typed an @", reads)
	}

	press(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	typeText(t, m, "@")
	press(t, m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	if frame := plain(m.Frame()); !strings.Contains(frame, "@doggo") || reads != 1 {
		t.Errorf("the first completion was not the pull request's own (reads=%d):\n%s", reads, frame)
	}

	for range len("@doggo") {
		press(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}

	typeText(t, m, "@wa")
	press(t, m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})

	if frame := plain(m.Frame()); !strings.Contains(frame, "@watch-doggo") {
		t.Errorf("a bot the repository has seen was not offered:\n%s", frame)
	}
}
