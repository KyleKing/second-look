package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

// t is the conversations alone: every open thread under the line it answers,
// with the rest of the diff left out. GitHub's own list interleaves the diff,
// so what is still being asked spreads through a page of code nobody is
// rereading.
//
// It is off the c cycle, because a conversation is what the forge already holds
// rather than what this pass is staging, and leaving it goes back to the diff.
func TestTheThreadsViewCarriesTheLineEachAnswers(t *testing.T) {
	t.Parallel()

	open := []threads.Thread{{
		Path: parsed, Side: artifact.SideRight, Line: 15,
		Notes: []threads.Note{{ID: 77, Author: "KyleKing", Body: "does this handle nil?"}},
	}}

	_, path, _ := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 14, "check err"))
	m := tui.New(t.Context(), reviewAt(t, path), diff.Parse([]byte(patch)), path,
		func(context.Context, *artifact.Review) (string, error) { return "", nil },
		tui.WithThreads(open))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	press(m, tea.KeyPressMsg{Code: 't', Text: "t"})

	frame := plain(m.Frame())

	// The line the thread hangs from is drawn above it, and it is a line of the
	// diff rather than quoted text, so it keeps its number and its grammar.
	for _, want := range []string{"line 15 · 1 comment", "does this handle nil?", "lines, err := split(r)"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the threads view never said %q:\n%s", want, frame)
		}
	}

	// What this pass is staging is not a conversation, so it is left out.
	if strings.Contains(frame, "check err") {
		t.Errorf("a staged comment was drawn in the threads view:\n%s", frame)
	}

	press(m, tea.KeyPressMsg{Code: 't', Text: "t"})

	if got := plain(m.Frame()); !strings.Contains(got, "check err") {
		t.Errorf("leaving the threads view did not come back to the diff:\n%s", got)
	}
}
