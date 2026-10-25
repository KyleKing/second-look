package tui_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/labels"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

var errLabelRefused = errors.New("label refused")

type labelCalls struct {
	mu    sync.Mutex
	calls []string
}

func (c *labelCalls) labeling() tui.Labeling {
	return tui.Labeling{
		Read: func(context.Context, string) ([]labels.Label, error) {
			return []labels.Label{
				{Name: "area: api", Color: "0e8a16", Description: "touches the HTTP surface"},
				{Name: "bug", Color: "d73a4a"},
				{Name: "refused", Color: "ededed"},
				{Name: "watch-doggo-fix", Color: "5319e7"},
			}, nil
		},
		Set: func(_ context.Context, at labels.Target, name string, on bool) error {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.calls = append(c.calls, strings.Join(labels.SetArgs(at, name, on), " "))

			if name == "refused" {
				return errLabelRefused
			}

			return nil
		},
	}
}

// feed runs a command and hands what it answers to the model, descending into
// batches the way the runtime does.
func feed(t *testing.T, m *tui.Model, cmd tea.Cmd) {
	t.Helper()

	if cmd == nil {
		return
	}

	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			feed(t, m, c)
		}
	case nil:
	default:
		_, next := m.Update(msg)
		feed(t, m, next)
	}
}

func typeText(t *testing.T, m *tui.Model, text string) {
	t.Helper()

	for _, r := range text {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		feed(t, m, cmd)
	}
}

// labeled is a review of a pull request already carrying one label, with the
// picker open on the repository's labels.
func labeled(t *testing.T, c *labelCalls) *tui.Model {
	t.Helper()

	m, _, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}, patch, tui.WithAbout(threads.About{Title: "carry the split's error", Labels: []string{"bug"}}),
		tui.WithLabeling(c.labeling()))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if frame := plain(m.Frame()); !strings.Contains(frame, "reading the labels of kyleking/jj-diff") {
		t.Errorf("the picker did not say it was reading:\n%s", frame)
	}

	feed(t, m, cmd)

	if frame := plain(m.Frame()); !strings.Contains(frame, "touches the HTTP surface") {
		t.Errorf("the picker did not list the repository's labels:\n%s", frame)
	}

	return m
}

// l opens the picker over the repository's labels, typing narrows them, and
// enter flips the one under the cursor: the header says so before the forge has
// answered, and a refusal takes it back and says why.
func TestLabelPickerTogglesAndRollsBack(t *testing.T) {
	t.Parallel()

	const post = "api --method POST /repos/kyleking/jj-diff/issues/42/labels -f labels[]="

	tests := []struct {
		name   string
		typed  string
		header string
		gone   string
		call   string
		footer string
	}{
		{name: "adds", typed: "doggo", header: "[bug] [watch-doggo-fix]", call: post + "watch-doggo-fix"},
		{
			name: "removes", typed: "bug", gone: "[bug]",
			call: "api --method DELETE /repos/kyleking/jj-diff/issues/42/labels/bug",
		},
		{
			name: "rolls back a refusal", typed: "refused", header: "[bug]", gone: "[refused]",
			call: post + "refused", footer: "label refused",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := &labelCalls{}
			m := labeled(t, c)

			typeText(t, m, tc.typed)

			_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if frame := plain(m.Frame()); !strings.Contains(frame, tc.header) {
				t.Errorf("the header did not show %q before the answer:\n%s", tc.header, frame)
			}

			feed(t, m, cmd)
			press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

			frame := plain(m.Frame())
			if tc.gone != "" && strings.Contains(frame, tc.gone) {
				t.Errorf("%s is still in the header:\n%s", tc.gone, frame)
			}

			if !strings.Contains(frame, tc.footer) {
				t.Errorf("the footer never said %q:\n%s", tc.footer, frame)
			}

			if len(c.calls) != 1 || c.calls[0] != tc.call {
				t.Errorf("calls = %v, want [%s]", c.calls, tc.call)
			}
		})
	}
}
