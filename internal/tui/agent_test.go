package tui_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/acp"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// agentStub is the stand-in agent's path, built once for every test that
// drives it: a pane over a real subprocess is the same code path `devin acp`
// takes, and a go build per test spends most of the wait window compiling.
// The binary lives in a shared temp dir outliving any one test's cleanup.
var agentStub = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "agent-stub")
	if err != nil {
		return "", fmt.Errorf("making the stub's directory: %w", err)
	}

	bin := filepath.Join(dir, "agent-stub")

	//nolint:gosec // the only variable is the temp directory this test made
	out, err := exec.Command("go", "build", "-o", bin, "../acp/testdata/stub").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("building the stub agent: %w\n%s", err, out)
	}

	return bin, nil
})

func stubPath(t *testing.T) string {
	t.Helper()

	bin, err := agentStub()
	if err != nil {
		t.Fatal(err)
	}

	return bin
}

// loop is the program loop a test needs for a session: commands' answers come
// back on the one channel however long they take, so a turn still running when
// an earlier check passed is not stranded on a channel nobody drains.
type loop struct {
	m    *tui.Model
	msgs chan tea.Msg
}

func looped(m *tui.Model) *loop {
	return &loop{m: m, msgs: make(chan tea.Msg, 32)}
}

// feed runs a command's answer back into the loop.
func (l *loop) feed(cmd tea.Cmd) {
	if cmd == nil {
		return
	}

	go func() {
		if got := cmd(); got != nil {
			l.msgs <- got
		}
	}()
}

// key presses and pumps what it asked for.
func (l *loop) key(k tea.KeyPressMsg) {
	_, cmd := l.m.Update(k)
	l.feed(cmd)
}

// wait pumps until the model shows the state, or the deadline says it never
// will — what never arrives is the assertions' to say.
func (l *loop) wait(fn func() bool) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	deadline := time.After(15 * time.Second)

	for !fn() {
		select {
		case got := <-l.msgs:
			if batch, ok := got.(tea.BatchMsg); ok {
				for _, c := range batch {
					l.feed(c)
				}

				continue
			}

			_, next := l.m.Update(got)
			l.feed(next)
		case <-tick.C:
		case <-deadline:
			return
		}
	}
}

// Handing a todo set to a configured agent is a conversation, not a process
// that exits: the set goes over the session as the prompt, the transcript and
// the ask draw in the pane, and the session id lands on the review so the next
// open loads it back.
func TestAgentPaneCarriesAWholeTurn(t *testing.T) {
	t.Parallel()

	stub := stubPath(t)

	sub := &counter{}
	home := t.TempDir()
	path := filepath.Join(home, "pr-42.toml")
	review := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
		Comments: []artifact.Comment{comment("c1", parsed, artifact.SideRight, 15, "a finding")},
	}
	if err := artifact.Save(path, review); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), review, diff.Parse([]byte(patch)), path, sub.post,
		tui.WithStore(home),
		tui.WithAgent(func(ctx context.Context, loadID string) (*acp.Session, error) {
			return acp.Start(ctx, []string{stub}, home, loadID)
		}))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	go2(t, m, ']', 'c')
	state(t, m, 't')

	run := looped(m)
	run.key(letter('T'))
	run.wait(func() bool {
		frame := plain(m.Frame())

		return strings.Contains(frame, "1 Allow") && strings.Contains(frame, "stub heard:")
	})

	frame := plain(m.Frame())
	for _, want := range []string{
		"stub", "the agent has the keyboard", "runs the tests", "1 Allow", "2 Reject",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("the pane is missing %q:\n%s", want, frame)
		}
	}

	// A digit answers the ask; the turn ends on it.
	run.key(letter('1'))
	run.wait(func() bool {
		return strings.Contains(plain(m.Frame()), "agent done")
	})

	// The pane holds the tail; the start of the turn is a scroll up.
	for range 3 {
		run.key(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	}
	run.wait(func() bool {
		return strings.Contains(plain(m.Frame()), "stub heard:")
	})

	if frame := plain(m.Frame()); !strings.Contains(frame, "stub heard:") {
		t.Errorf("the scrolled transcript lost the turn's start:\n%s", frame)
	}

	// Esc leaves the pane, not the session: the title still carries its word.
	run.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if frame := plain(m.Frame()); !strings.Contains(frame, "agent done") {
		t.Errorf("the title dropped the ended turn:\n%s", frame)
	}

	// T again is the same conversation, reopened rather than restarted.
	run.key(letter('T'))

	if frame := plain(m.Frame()); !strings.Contains(frame, "selected allow") {
		t.Errorf("the reopened pane lost the transcript:\n%s", frame)
	}

	saved, err := artifact.Load(path)
	if err != nil {
		t.Fatalf("the review on disk: %v", err)
	}

	if saved.Agent.Session != "stub-session" {
		t.Errorf("the review recorded %q, not the session it ran", saved.Agent.Session)
	}

	if _, err := os.Stat(artifact.TodoPath(home, 42)); err != nil {
		t.Errorf("the todo set was never written: %v", err)
	}

	askedOverTheSession(t, run, path)
}

// An empty review has no todo set to hand back, so T asks the agent to draft
// one from the diff instead of refusing: "hand this to an agent" and "there
// is nothing here yet" mean the same thing on a review with no comments.
func TestDispatchOnAnEmptyReviewAsksTheAgentToDraftIt(t *testing.T) {
	t.Parallel()

	stub := stubPath(t)

	sub := &counter{}
	home := t.TempDir()
	path := filepath.Join(home, "pr-42.toml")
	review := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}
	if err := artifact.Save(path, review); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), review, diff.Parse([]byte(patch)), path, sub.post,
		tui.WithStore(home),
		tui.WithAgent(func(ctx context.Context, loadID string) (*acp.Session, error) {
			return acp.Start(ctx, []string{stub}, home, loadID)
		}))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	run := looped(m)
	run.key(letter('T'))
	run.wait(func() bool {
		return strings.Contains(plain(m.Frame()), "stub heard: Review kyleking/jj-diff#42")
	})

	if frame := plain(m.Frame()); !strings.Contains(frame, "stub heard: Review kyleking/jj-diff#42") {
		t.Errorf("T on an empty review did not ask the agent to draft it:\n%s", frame)
	}

	// A second T, still with nothing owed, resumes the same conversation
	// rather than asking it to draft the review all over again.
	run.key(letter('1'))
	run.wait(func() bool { return strings.Contains(plain(m.Frame()), "agent done") })
	run.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	run.key(letter('T'))

	if frame := plain(m.Frame()); strings.Count(frame, "stub heard: Review") != 1 {
		t.Errorf("T asked the agent to draft the review twice:\n%s", frame)
	}
}

// A pane reopened after esc must take a line the same as a fresh one. Its
// prompt is a new input each time, and a new input listens to nothing until
// it is focused — the tell is the placeholder, which draws whole only once
// the input carries the frame's width.
func TestAReopenedAgentPaneTakesALine(t *testing.T) {
	t.Parallel()

	stub := stubPath(t)

	sub := &counter{}
	home := t.TempDir()
	path := filepath.Join(home, "pr-42.toml")
	review := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}
	if err := artifact.Save(path, review); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), review, diff.Parse([]byte(patch)), path, sub.post,
		tui.WithStore(home),
		tui.WithAgent(func(ctx context.Context, loadID string) (*acp.Session, error) {
			return acp.Start(ctx, []string{stub}, home, loadID)
		}))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	run := looped(m)
	run.key(letter('T'))
	run.wait(func() bool {
		return strings.Contains(plain(m.Frame()), "1 Allow")
	})
	run.key(letter('1'))
	run.wait(func() bool { return strings.Contains(plain(m.Frame()), "agent done") })

	run.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	run.key(letter('T'))

	if frame := plain(m.Frame()); !strings.Contains(frame, "talk to stub") {
		t.Errorf("the reopened prompt lost its placeholder:\n%s", frame)
	}

	run.key(letter('x'))

	if frame := plain(m.Frame()); !strings.Contains(frame, "❯ x") {
		t.Errorf("the reopened prompt did not take the keystroke:\n%s", frame)
	}
}

// askedOverTheSession puts a question about a line to the live session, and
// checks the agent's reply in that turn alone lands in [plan].
func askedOverTheSession(t *testing.T, run *loop, path string) {
	t.Helper()

	m := run.m
	run.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	for _, k := range "]hjAl" + "why one?" {
		run.key(letter(k))
	}

	run.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	run.wait(func() bool { return strings.Contains(plain(m.Frame()), "1 Allow") })
	run.key(letter('1'))

	var answer string

	run.wait(func() bool {
		saved, err := artifact.Load(path)
		if err == nil && len(saved.Plan.Asks) == 1 {
			answer = saved.Plan.Asks[0].Answer
		}

		return answer != ""
	})

	if !strings.HasPrefix(answer, "stub heard: A reviewer") || strings.Contains(answer, "fix") {
		t.Errorf("the ask was answered %q, want this turn's reply alone", answer)
	}
}
