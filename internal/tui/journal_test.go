package tui_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/journal"
	"github.com/kyleking/second-look/internal/tui"
)

// The journal hears every command a screen answers and nothing a text field
// was typing: a filter's letters and a search's are not keys anyone reached
// for. What it hears is the answer to "which of these did I use".
func TestJournalHearsCommandsAndNotText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 16, "check err"))
	s := tui.NewShell(t.Context(), chooses(t), func() tui.Reviewer {
		return func(context.Context) (*tui.Model, error) { return m, nil }
	}).WithJournal(journal.New(&buf))

	pump(t, s, s.Init())

	esc := tea.KeyPressMsg{Code: tea.KeyEscape}

	sendTo(t, s, letter('j'))
	sendTo(t, s, letter('/')) // opening the filter is a command
	sendTo(t, s, letter('x')) // what it typed is not
	sendTo(t, s, esc)
	sendTo(t, s, enter())
	m.HeadChecked("deadbeef")
	sendTo(t, s, letter('j'))
	sendTo(t, s, letter('/')) // the review's search, the same rule
	sendTo(t, s, letter('x'))
	sendTo(t, s, esc)
	sendTo(t, s, letter('q'))

	type event struct{ S, E, W string }
	var got []event

	for l := range strings.Lines(strings.TrimSpace(buf.String())) {
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
		{"queue", "key", "j"},
		{"queue", "key", "/"},
		{"queue", "key", "enter"},
		{"shell", "open", ""},
		{"review", "head", ""},
		{"review", "key", "j"},
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
