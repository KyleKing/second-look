package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// drain runs a command the way the program's command runner does, feeding
// every message it lands back through the model until nothing answers more.
func drain(m *tui.Model, cmd tea.Cmd) {
	for cmd != nil {
		var next tea.Cmd

		for _, msg := range runCmd(cmd) {
			_, c := m.Update(msg)
			next = tea.Batch(next, c)
		}

		cmd = next
	}
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, runCmd(c)...)
		}

		return out
	default:
		return []tea.Msg{msg}
	}
}

// A view switch on a diff past the async row floor builds off the input
// goroutine, names the wait, and answers a key typed mid-build after it
// lands rather than racing what it was reading.
func TestTheBigSwitchBuildsOffTheKeys(t *testing.T) {
	t.Parallel()

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Comments: []artifact.Comment{{
			ID: "a", Path: "internal/pkg000/file.go", Line: 5, Side: artifact.SideRight,
			Body: "rename this", Status: artifact.StatusReady,
		}},
	}, hugePatch())

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	drain(m, cmd)

	// v asks for the code view, and on a diff this size the switch builds
	// where a key cannot feel it.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})

	if !m.Building() {
		t.Fatalf("the switch built where the key arrived:\n%s", plain(m.Frame()))
	}
	if got := plain(m.Frame()); !strings.Contains(got, "building") {
		t.Fatalf("the wait was not named:\n%s", got)
	}

	// c asked while the build was reading the model waits for its settle and
	// is answered then, rather than mutating what it was reading.
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	drain(m, cmd)

	if got := plain(m.Frame()); !strings.Contains(got, "rename this") {
		t.Fatalf("the waited-on key did not land on the comments:\n%s", got)
	}
}
