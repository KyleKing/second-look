package acp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/aragonite/hermetic"

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

	code := hermetic.Run(m, hermetic.App("second-look"))

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

// expectTranscript checks what one answered turn left on the transcript: the
// reply and the permission answer as agent text, the prompt echoed, and the
// tool call carrying its status and its output.
func expectTranscript(t *testing.T, s *acp.Session) {
	t.Helper()

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

	if tool := toolEntry(t, s); tool.Detail != "the suite passed" {
		t.Errorf("the tool call kept %q for detail, not its output", tool.Detail)
	}
}

// A turn is the whole of it: the prompt echoes, the tool call lands, the
// permission ask waits for a real answer, and the stop reason ends it.
func TestSessionStreamsAWholeTurn(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "", "")
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

	ended := turnThrough(t, s, "fix it")

	if ended.err != nil {
		t.Fatalf("the turn failed: %v", ended.err)
	}

	if ended.reason != "end_turn" {
		t.Errorf("the turn stopped %q", ended.reason)
	}

	expectTranscript(t, s)

	// A turn is two messages — the reply and what the permission answer said —
	// and a second turn's pair lands after them as entries of their own.
	turnThrough(t, s, "and again")

	if got := entryTexts(s, acp.Agent); len(got) != 4 || got[2] != "stub heard: and again" {
		t.Errorf("the second turn's reply reads back as %q", got)
	}

	if got := entryTexts(s, acp.End); len(got) != 2 || got[0] != "end_turn" || got[1] != "end_turn" {
		t.Errorf("the transcript closed the turns as %v", got)
	}
}

// A turn the caller walks away from still records its end: a canceled context
// closes it as interrupted rather than leaving the last word open, and a
// cancel the agent answers closes it the way the wire said.
func TestAnEndedTurnStillCloses(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}
	defer s.Close()

	ctx, stop := context.WithCancel(t.Context())

	done := promptOn(ctx, s, "hold")

	until(t, s, func() bool { return len(entryTexts(s, acp.Agent)) == 1 })
	stop()
	expectReason(t, done, "interrupted", "a walked-away turn")

	done = promptOn(t.Context(), s, "hold")

	// Busy() is set before the prompt is on the wire, so the cancel has to
	// wait on the stub's own word that this turn opened: an agent reply that
	// follows the second prompt rather than the first.
	until(t, s, func() bool { return turnAcked(s, 2) })

	if err := s.Cancel(t.Context()); err != nil {
		t.Fatalf("asking the agent to stop: %v", err)
	}

	expectReason(t, done, "canceled", "a canceled turn")

	if got := entryTexts(s, acp.End); len(got) != 2 || got[0] != "interrupted" || got[1] != "canceled" {
		t.Errorf("the transcript closed the turns as %v", got)
	}
}

// promptOn runs a turn on its own goroutine and answers with the reason it
// closed — or "errored:" and why — on the channel it hands back.
func promptOn(ctx context.Context, s *acp.Session, text string) chan string {
	done := make(chan string, 1)

	go func() {
		reason, err := s.Prompt(ctx, text)
		if err != nil {
			reason = "errored: " + err.Error()
		}

		done <- reason
	}()

	return done
}

// turnAcked is whether the nth prompt's reply has landed, which is the wire's
// own word that the turn the test is about to cancel actually opened.
func turnAcked(s *acp.Session, yous int) bool {
	seen := 0
	for _, e := range s.Entries() {
		if e.Kind == acp.You {
			seen++
		}

		if e.Kind == acp.Agent && seen == yous {
			return true
		}
	}

	return false
}

// expectReason waits out a turn and checks the reason it closed with.
func expectReason(t *testing.T, done <-chan string, want, what string) {
	t.Helper()

	select {
	case reason := <-done:
		if reason != want {
			t.Errorf("%s closed %q", what, reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never closed", what)
	}
}

type turn struct {
	reason string
	err    error
}

// turnThrough prompts the stub and allows the permission it asks for.
func turnThrough(t *testing.T, s *acp.Session, text string) turn {
	t.Helper()

	done := make(chan turn, 1)
	go func() {
		reason, err := s.Prompt(t.Context(), text)
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

	select {
	case ended := <-done:
		return ended
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never ended")
	}

	return turn{}
}

// A review that already carried a session hands its id back, and an agent
// that can load replays what it had rather than starting over.
func TestSessionLoadsARecordedOne(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "old-session", "")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}
	defer s.Close()

	if s.ID() != "old-session" {
		t.Errorf("the session id is %q; the recorded one was not kept", s.ID())
	}

	awaitEntry(t, s, acp.Agent, "reloaded transcript")

	// The replay's own prompts carry their kind, so the transcript keeps the
	// turn each answer belongs to.
	want := []acp.Entry{
		{Kind: acp.You, Text: "what was asked before"},
		{Kind: acp.You, Text: "and after that"},
		{Kind: acp.You, Text: "one prompt in two chunks"},
		{Kind: acp.Agent, Text: "reloaded transcript"},
	}
	if got := s.Entries(); !slices.Equal(got, want) {
		t.Errorf("the replay reads %+v, want %+v", got, want)
	}
}

// The adapter ending has to reach the reader: a pane watching Dead would
// otherwise sit on a transcript the process can no longer write to.
func TestSessionReportsTheAdapterDying(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "", "")
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

// A configured mode goes over session/set_mode once the session is up, and
// one the agent never advertised is said on the transcript instead of being
// applied to nothing.
func TestStartAsksForTheConfiguredMode(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "", "bypass")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}
	defer s.Close()

	if s.Mode() != "Bypass Permissions" {
		t.Errorf("the session mode reads %q", s.Mode())
	}

	modes := s.Modes()
	if len(modes) != 2 || modes[0].ID != "code" || modes[1].ID != "bypass" {
		t.Errorf("the advertised modes read back as %v", modes)
	}

	awaitEntry(t, s, acp.Note, "mode: Bypass Permissions")
}

func TestStartNamesAModeTheAgentDoesNotOffer(t *testing.T) {
	t.Parallel()

	s, err := acp.Start(t.Context(), []string{stub}, t.TempDir(), "", "yolo")
	if err != nil {
		t.Fatalf("starting the stub: %v", err)
	}
	defer s.Close()

	if s.Mode() != "Code" {
		t.Errorf("an unapplied mode still reads %q", s.Mode())
	}

	awaitEntry(t, s, acp.Note, `"yolo" is not one stub offers`)
}

// awaitEntry waits for a transcript entry of kind carrying text.
func awaitEntry(t *testing.T, s *acp.Session, kind acp.EntryKind, text string) {
	t.Helper()

	until(t, s, func() bool {
		for _, e := range s.Entries() {
			if e.Kind == kind && strings.Contains(e.Text, text) {
				return true
			}
		}

		return false
	})
}
