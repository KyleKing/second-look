package tui_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

var errLabel = errors.New("label refused")

type handoffs struct {
	mu     sync.Mutex
	labels []string
	sets   []string
}

func (h *handoffs) label(_ context.Context, _ *artifact.Review, label string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.labels = append(h.labels, label)

	if label == "refused" {
		return errLabel
	}

	return nil
}

func (h *handoffs) dispatch(_ context.Context, set, _ string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sets = append(h.sets, set)

	return "", nil
}

func todoModel(t *testing.T, h *handoffs, fixes tui.Fixes) (*tui.Model, *counter) {
	t.Helper()

	reply := artifact.Comment{
		ID: "reply-1500", Path: parsed, Side: artifact.SideRight, Line: 15, InReplyTo: 1500,
		ThreadID: "PRRT_a", Body: "will fix: wrap it", Status: artifact.StatusTodo,
	}

	m, _, sub := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: []artifact.Comment{reply},
	}, patch,
		tui.WithThreads([]threads.Thread{finding("a", 15, "doggo", "wrap the error with the path")}),
		tui.WithStore(t.TempDir()), tui.WithDispatcher(h.dispatch), tui.WithFixes(fixes))
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})

	return m, sub
}

// A todo reply posts like any reply, and the submit chord asks who gets the fix
// before it does.
func TestSubmitAsksWhoGetsTheFixes(t *testing.T) {
	t.Parallel()

	prefix := "S r  1 todo  fixes go to  [w]atch-doggo  [l]ocal claude  [n]obody  [enter] "

	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		label  string
		cloud  bool
		posts  int
		labels []string
		sets   int
		footer string
	}{
		{name: "asks", posts: 0, footer: prefix + "local claude"},
		{name: "asks with the owner's default", cloud: true, posts: 0, footer: prefix + "watch-doggo"},
		{
			name: "watch-doggo labels the pull request", keys: []tea.KeyPressMsg{key('w')},
			posts: 1, labels: []string{"watch-doggo-fix"}, footer: "posted 3 comments · watch-doggo-fix added",
		},
		{
			name: "local hands the findings to the agent", keys: []tea.KeyPressMsg{key('l')},
			posts: 1, sets: 1, footer: "posted 3 comments · handed 1 todo to the local agent",
		},
		{name: "nobody only posts", keys: []tea.KeyPressMsg{key('n')}, posts: 1, footer: "posted 3 comments"},
		{
			name: "enter takes the local default", keys: []tea.KeyPressMsg{{Code: tea.KeyEnter}},
			posts: 1, sets: 1, footer: "handed 1 todo to the local agent",
		},
		{
			name: "enter takes the owner's default", cloud: true, keys: []tea.KeyPressMsg{{Code: tea.KeyEnter}},
			posts: 1, labels: []string{"watch-doggo-fix"}, footer: "watch-doggo-fix added",
		},
		{
			name: "esc backs out", keys: []tea.KeyPressMsg{{Code: tea.KeyEscape}},
			posts: 0, footer: "canceled, nothing was posted",
		},
		{
			name: "a failed label is said after the post", keys: []tea.KeyPressMsg{key('w')}, label: "refused",
			posts: 1, labels: []string{"refused"}, footer: "posted 3 comments · hand-off failed: label refused",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &handoffs{}

			label := tc.label
			if label == "" {
				label = "watch-doggo-fix"
			}

			m, sub := todoModel(t, h, tui.Fixes{Add: h.label, Label: label, ToWatchDoggo: tc.cloud})

			press(t, m, key('S'))
			press(t, m, key('r'))

			for _, k := range tc.keys {
				press(t, m, k)
			}

			if sub.n != tc.posts || len(h.labels) != len(tc.labels) || len(h.sets) != tc.sets {
				t.Errorf("posts=%d labels=%v sets=%d, want posts=%d labels=%v sets=%d",
					sub.n, h.labels, len(h.sets), tc.posts, tc.labels, tc.sets)
			}

			if got := plain(m.Frame()); !strings.Contains(got, tc.footer) {
				t.Errorf("the footer never said %q:\n%s", tc.footer, got)
			}

			requireFinding(t, h.sets)
		})
	}
}

func requireFinding(t *testing.T, sets []string) {
	t.Helper()

	for _, set := range sets {
		body, err := os.ReadFile(set) //nolint:gosec // the model wrote it under this test's own store
		if err != nil || !strings.Contains(string(body), "wrap the error with the path") {
			t.Errorf("the agent was handed %s without the finding: %v", set, err)
		}
	}
}

// P on a todo reply stops at the same hand-off choice S does, and the reply
// goes out on its own once it is made.
func TestPostingOneTodoReplyAsksWhoGetsTheFix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		sent   int
		labels []string
		sets   int
		footer string
	}{
		{name: "asks", sent: 0, footer: "P  1 todo  fixes go to  [w]atch-doggo  [l]ocal claude  [n]obody"},
		{
			name: "watch-doggo labels the pull request", keys: []tea.KeyPressMsg{key('w')},
			sent: 1, labels: []string{"watch-doggo-fix"}, footer: "posted reply-1500 · watch-doggo-fix added",
		},
		{
			name: "local hands the finding to the agent", keys: []tea.KeyPressMsg{key('l')},
			sent: 1, sets: 1, footer: "posted reply-1500 · handed 1 todo to the local agent",
		},
		{name: "nobody only posts", keys: []tea.KeyPressMsg{key('n')}, sent: 1, footer: "posted reply-1500"},
		{name: "esc backs out", keys: []tea.KeyPressMsg{{Code: tea.KeyEscape}}, footer: "canceled, nothing was posted"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &handoffs{}
			m, _ := todoModel(t, h, tui.Fixes{Add: h.label, Label: "watch-doggo-fix"})

			var sent []string

			m.SetSender(func(_ context.Context, _ *artifact.Review, id string) (string, error) {
				sent = append(sent, id)

				return "posted " + id, nil
			})

			go2(t, m, ']', 'c')
			press(t, m, key('P'))

			for _, k := range tc.keys {
				press(t, m, k)
			}

			if len(sent) != tc.sent || len(h.labels) != len(tc.labels) || len(h.sets) != tc.sets {
				t.Errorf("sent=%v labels=%v sets=%d, want sent=%d labels=%v sets=%d",
					sent, h.labels, len(h.sets), tc.sent, tc.labels, tc.sets)
			}

			if got := plain(m.Frame()); !strings.Contains(got, tc.footer) {
				t.Errorf("the footer never said %q:\n%s", tc.footer, got)
			}

			requireFinding(t, h.sets)
		})
	}
}
