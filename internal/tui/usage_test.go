package tui_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/keyusage"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// The log hears every command a screen answers, under the context the key
// landed in, and nothing a text field was typing: a filter's letters and a
// search's are not keys anyone reached for.
func TestUsageLogsCommandsByContextAndNotText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := keyusage.Open("second-look", keyusage.WithDir(dir))

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 16, "check err"))
	s := tui.NewShell(t.Context(), chooses(t), func() tui.Reviewer {
		return func(context.Context) (*tui.Model, error) { return m, nil }
	}).WithUsage(rec, nil)

	pump(t, s, s.Init())

	esc := tea.KeyPressMsg{Code: tea.KeyEscape}

	sendTo(t, s, letter('j'))
	sendTo(t, s, letter('/')) // opening the filter is a command
	sendTo(t, s, letter('x')) // what it typed is not
	sendTo(t, s, esc)
	sendTo(t, s, enter())
	m.HeadChecked("deadbeef")
	sendTo(t, s, letter('j'))
	sendTo(t, s, letter('?')) // the legend owns the keys that follow
	sendTo(t, s, letter('j'))
	sendTo(t, s, esc)
	sendTo(t, s, letter('/')) // the review's search, the same rule
	sendTo(t, s, letter('x'))
	sendTo(t, s, esc)
	sendTo(t, s, letter('q'))

	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("want one log file, got %v (%v)", files, err)
	}

	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}

	type event struct{ S, E, W string }
	var got []event

	for l := range strings.Lines(strings.TrimSpace(string(raw))) {
		var e struct {
			S string `json:"s"`
			E string `json:"e"`
			W string `json:"w"`
		}
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("line %q is not JSON: %v", l, err)
		}

		got = append(got, event{e.S, e.E, e.W})
	}

	want := []event{
		{"queue/conversations", "key", "/"},
		{"queue/conversations", "key", "enter"},
		{"shell", "open", ""},
		{"review", "head", ""},
		{"review", "key", "?"},
		{"review/help", "key", "esc"},
		{"review", "key", "/"},
		{"review", "key", "q"},
		{"shell", "leave", ""},
	}

	if len(got) != len(want) {
		t.Fatalf("the sitting journaled %v, want %v", got, want)
	}

	for i, w := range want {
		if got[i] != w {
			t.Errorf("event %d was %v, want %v — the whole log: %v", i, got[i], w, got)
		}
	}
}
