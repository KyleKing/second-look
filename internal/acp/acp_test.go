package acp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/acp"
)

// stub is the built stand-in agent, built once for the whole package. The
// tests drive the real client over real stdio against it, which is the same
// code path an ACP adapter takes, subprocess included.
var stub string //nolint:gochecknoglobals // built once in TestMain for every test here

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "acp-stub")
	if err != nil {
		panic(err)
	}

	stub = filepath.Join(dir, "stub")

	//nolint:gosec // the only variable is the temp directory this test made
	build := exec.CommandContext(context.Background(), "go", "build", "-o", stub, "./testdata/stub")

	out, err := build.CombinedOutput()
	if err != nil {
		panic(string(out))
	}

	code := m.Run()

	//nolint:errcheck // a temp directory that outlives the test is the OS's problem
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// until polls rather than sleeping, because the agent draws when it draws.
// Changed() is drained each pass so a wake arriving mid-poll is not missed.
func until(t *testing.T, s *acp.Session, want func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if want() {
			return
		}

		select {
		case <-s.Changed():
		case <-time.After(20 * time.Millisecond):
		}
	}

	t.Fatal("the session never reached the state the test waited on")
}

func entryTexts(s *acp.Session, kind acp.EntryKind) []string {
	var out []string

	for _, e := range s.Entries() {
		if e.Kind == kind {
			out = append(out, e.Text)
		}
	}

	return out
}

func toolEntry(t *testing.T, s *acp.Session) acp.Entry {
	t.Helper()

	for _, e := range s.Entries() {
		if e.Kind == acp.Tool {
			return e
		}
	}

	t.Fatal("no tool call landed on the transcript")

	return acp.Entry{}
}

// A turn is the whole of it: the prompt echoes, the tool call lands, the
// permission ask waits for a real answer, and the stop reason ends it.
func TestSessionStreamsAWholeTurn(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}
	defer s.Close()

	if s.Name() != "stub" {
		t.Errorf("the agent named itself %q", s.Name())
	}

	if s.ID() != "stub-session" {
		t.Errorf("the session id is %q", s.ID())
	}

	type turn struct {
		reason string
		err    error
	}

	done := make(chan turn, 1)
	go func() {
		reason, err := s.Prompt(t.Context(), "fix it")
		done <- turn{reason, err}
	}()

	var asked *acp.Permit
	until(t, s, func() bool {
		asked = s.Pending()
		return asked != nil
	})

	if len(asked.Options) != 2 {
		t.Fatalf("the ask carried %d options", len(asked.Options))
	}

	asked.Answer("allow")

	var ended turn

	select {
	case ended = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never ended")
	}

	if ended.err != nil {
		t.Fatalf("the turn failed: %v", ended.err)
	}

	if ended.reason != "end_turn" {
		t.Errorf("the turn stopped %q", ended.reason)
	}

	said := strings.Join(entryTexts(s, acp.Agent), "\n")
	if !strings.Contains(said, "stub heard: fix it") || !strings.Contains(said, "permission said: selected allow") {
		t.Errorf("the transcript does not carry the turn:\n%s", said)
	}

	if got := entryTexts(s, acp.You); len(got) != 1 || got[0] != "fix it" {
		t.Errorf("the prompt reads back as %v", got)
	}

	if tool := toolEntry(t, s); tool.Text != "runs the tests" || tool.Status != "completed" {
		t.Errorf("the tool call reads %q / %q", tool.Text, tool.Status)
	}
}

// A review that already carried a session hands its id back, and an agent
// that can load replays what it had rather than starting over.
func TestSessionLoadsARecordedOne(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "old-session")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}
	defer s.Close()

	if s.ID() != "old-session" {
		t.Errorf("the session id is %q; the recorded one was not kept", s.ID())
	}

	until(t, s, func() bool {
		for _, e := range s.Entries() {
			if e.Kind == acp.Agent && strings.Contains(e.Text, "reloaded transcript") {
				return true
			}
		}

		return false
	})
}

// The adapter ending has to reach the reader: a pane watching Dead would
// otherwise sit on a transcript the process can no longer write to.
func TestSessionReportsTheAdapterDying(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}

	s.Close()

	select {
	case err := <-s.Dead():
		if err != nil {
			t.Errorf("a deliberate close reported %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the adapter's end was never reported")
	}
}
