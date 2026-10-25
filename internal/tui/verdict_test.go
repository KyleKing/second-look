package tui_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

func finding(id string, line int, author, body string) threads.Thread {
	return threads.Thread{
		ID: "PRRT_" + id, Path: parsed, Side: artifact.SideRight, Line: line,
		Notes: []threads.Note{{ID: int64(line) * 100, Author: author, Body: body}},
	}
}

type prompts struct {
	mu   sync.Mutex
	seen []string
}

func (p *prompts) asker(answer string) tui.Asker {
	return func(_ context.Context, prompt string) (string, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		p.seen = append(p.seen, prompt)

		return answer, nil
	}
}

func judging(t *testing.T, asker tui.Asker, staged []artifact.Comment, open ...threads.Thread) (*tui.Model, string) {
	t.Helper()

	opts := []tui.Option{tui.WithThreads(open)}
	if asker != nil {
		opts = append(opts, tui.WithAsker(asker))
	}

	m, path, _ := modelFor(t, &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: staged,
	}, patch, opts...)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	return m, path
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

type verdictCase struct {
	name     string
	verdict  rune
	staged   []artifact.Comment
	status   string
	resolve  bool
	brief    string
	wantBody string
}

func replyTo(t *testing.T, path string, id int64) artifact.Comment {
	t.Helper()

	cs := reviewAt(t, path).Comments
	for i := range cs {
		if cs[i].InReplyTo == id {
			return cs[i]
		}
	}

	t.Fatalf("no reply to %d is staged", id)

	return artifact.Comment{}
}

func (tt verdictCase) check(t *testing.T, reply artifact.Comment, asked *prompts) {
	t.Helper()

	if reply.Status != tt.status || reply.Resolve != tt.resolve || reply.Body != tt.wantBody {
		t.Errorf("reply = %q resolve=%v body=%q, want %q resolve=%v body=%q",
			reply.Status, reply.Resolve, reply.Body, tt.status, tt.resolve, tt.wantBody)
	}

	if reply.ThreadID != "PRRT_a" {
		t.Errorf("the reply forgot its thread, so nothing could resolve it: %q", reply.ThreadID)
	}

	if tt.brief == "" {
		if len(asked.seen) != 0 {
			t.Errorf("an agent was asked when the verdict needed no text: %q", asked.seen)
		}

		return
	}

	if len(asked.seen) != 1 || !strings.Contains(asked.seen[0], tt.brief) ||
		!strings.Contains(asked.seen[0], "wrap the error with the path") {
		t.Errorf("the prompt does not carry the brief and the finding: %q", asked.seen)
	}
}

// A verdict on a thread is the state of its staged reply, and where the
// reviewer has typed nothing the agent drafts the text for it.
func TestAVerdictOnAThreadStagesItsReply(t *testing.T) {
	t.Parallel()

	typed := artifact.Comment{
		ID: "reply-1500", Path: parsed, Side: artifact.SideRight, Line: 15, InReplyTo: 1500,
		Body: "typed by me", Severity: "question", Status: artifact.StatusDraft,
	}

	for _, tt := range []verdictCase{
		{"ready asks a question", 'r', nil, artifact.StatusReady, false, "ask the one question", "drafted"},
		{"todo acknowledges", 't', nil, artifact.StatusTodo, false, `"will fix:"`, "drafted"},
		{"resolve gives a reason", 'R', nil, artifact.StatusReady, true, "won't fix because", "drafted"},
		{"skip posts and asks nothing", 'x', nil, artifact.StatusSkip, false, "", ""},
		{"typed text is kept", 'R', []artifact.Comment{typed}, artifact.StatusReady, true, "", "typed by me"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var asked prompts

			m, path := judging(t, asked.asker("drafted"), tt.staged,
				finding("a", 15, "watch-doggo", "wrap the error with the path"))

			if got := plain(m.Frame()); !strings.Contains(got, "◆ 1 to judge") {
				t.Fatalf("the header does not count the finding:\n%s", got)
			}

			press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
			press(t, m, key(tt.verdict))

			tt.check(t, replyTo(t, path, 1500), &asked)

			if got := plain(m.Frame()); strings.Contains(got, "to judge") {
				t.Errorf("a ruled thread is still counted:\n%s", got)
			}
		})
	}
}

// With no agent the verdict still applies, and the card says what is missing
// rather than posting an empty reply.
func TestAVerdictWithNoAgentLeavesTheReplyToWrite(t *testing.T) {
	t.Parallel()

	m, path := judging(t, nil, nil, finding("a", 15, "watch-doggo", "wrap the error with the path"))

	press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	press(t, m, key('R'))

	if got := plain(m.Frame()); !strings.Contains(got, "no reply text, [e]dit to write one") {
		t.Errorf("the card does not say the reply is unwritten:\n%s", got)
	}

	if got := reviewAt(t, path).Comments[0]; got.Body != "" || !got.Resolve {
		t.Errorf("reply = %+v, want a resolving reply with no text", got)
	}
}

// The frames a reviewer judges from: a finding ruled on with its drafted
// reply, the lit key in the foot, and the verdict on a folded thread's rule.
// Tab walks the findings in diff order.
func TestVerdictFrames(t *testing.T) {
	t.Parallel()

	for _, width := range []int{80, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			var asked prompts

			m, _ := judging(t, asked.asker("will fix: wrap it with the path."), nil,
				finding("a", 15, "bot-a", "wrap the error with the path"),
				finding("b", 17, "bot-b", "this branch is unreachable"))
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})

			press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

			if got := m.CursorText(); !strings.Contains(got, "@bot-a") {
				t.Fatalf("tab began at %q, not the first finding in diff order", got)
			}

			press(t, m, key('t'))
			press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
			press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

			if got := m.CursorText(); !strings.Contains(got, "@bot-b") {
				t.Fatalf("tab walked to %q, not the second finding", got)
			}

			go2(t, m, 'z', 'a')

			golden.RequireEqual(t, []byte(plain(m.Frame())))
		})
	}
}
