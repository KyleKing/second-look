package tui_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

type handoffs struct {
	mu   sync.Mutex
	sets []string
}

func (h *handoffs) dispatch(_ context.Context, set, _ string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sets = append(h.sets, set)

	return "", nil
}

func todoModel(t *testing.T, h *handoffs, dispatch bool) (*tui.Model, *counter) {
	t.Helper()

	reply := artifact.Comment{
		ID: "reply-1500", Path: parsed, Side: artifact.SideRight, Line: 15, InReplyTo: 1500,
		ThreadID: "PRRT_a", Body: "will fix: wrap it", Status: artifact.StatusTodo,
	}

	opts := []tui.Option{
		tui.WithThreads([]threads.Thread{finding("a", 15, "doggo", "wrap the error with the path")}),
		tui.WithStore(t.TempDir()),
	}

	if dispatch {
		opts = append(opts, tui.WithDispatcher(h.dispatch))
	}

	m, _, sub := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: []artifact.Comment{reply},
	}, patch, opts...)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})

	return m, sub
}

// A todo reply posts like any reply, and the submit chord asks who gets the fix
// before it does.
func TestSubmitAsksWhoGetsTheFixes(t *testing.T) {
	t.Parallel()

	prefix := "S r  1 todo  fixes go to  [l]ocal agent  [n]obody  [enter] "

	tests := []struct {
		name    string
		keys    []tea.KeyPressMsg
		noAgent bool
		posts   int
		sets    int
		footer  string
	}{
		{name: "asks", posts: 0, footer: prefix + "local agent"},
		{name: "asks with no agent configured", noAgent: true, posts: 0, footer: prefix + "nobody"},
		{
			name: "local hands the findings to the agent", keys: []tea.KeyPressMsg{key('l')},
			posts: 1, sets: 1, footer: "posted 3 comments · handed 1 todo to the local agent",
		},
		{name: "nobody only posts", keys: []tea.KeyPressMsg{key('n')}, posts: 1, footer: "posted 3 comments"},
		{
			name: "enter takes the local agent", keys: []tea.KeyPressMsg{{Code: tea.KeyEnter}},
			posts: 1, sets: 1, footer: "handed 1 todo to the local agent",
		},
		{
			name: "enter with no agent configured posts and hands nothing on", noAgent: true,
			keys: []tea.KeyPressMsg{{Code: tea.KeyEnter}}, posts: 1, footer: "posted 3 comments",
		},
		{
			name: "esc backs out", keys: []tea.KeyPressMsg{{Code: tea.KeyEscape}},
			posts: 0, footer: "canceled, nothing was posted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &handoffs{}
			m, sub := todoModel(t, h, !tc.noAgent)

			press(t, m, key('S'))
			press(t, m, key('r'))

			for _, k := range tc.keys {
				press(t, m, k)
			}

			if sub.n != tc.posts || len(h.sets) != tc.sets {
				t.Errorf("posts=%d sets=%d, want posts=%d sets=%d", sub.n, len(h.sets), tc.posts, tc.sets)
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
		sets   int
		footer string
	}{
		{name: "asks", sent: 0, footer: "P  1 todo  fixes go to  [l]ocal agent  [n]obody"},
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
			m, _ := todoModel(t, h, true)

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

			if len(sent) != tc.sent || len(h.sets) != tc.sets {
				t.Errorf("sent=%v sets=%d, want sent=%d sets=%d", sent, len(h.sets), tc.sent, tc.sets)
			}

			if got := plain(m.Frame()); !strings.Contains(got, tc.footer) {
				t.Errorf("the footer never said %q:\n%s", tc.footer, got)
			}

			requireFinding(t, h.sets)
		})
	}
}
