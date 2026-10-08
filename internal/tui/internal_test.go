package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// paneWait polls the emulated screen rather than sleeping, because the child
// draws when it draws. It is the same shape as the e2e screen's await.
func paneWait(t *testing.T, p *pane, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.emu.String(), want) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("the pane never drew %q; it holds:\n%s", want, p.emu.String())
}

// paneGone waits out the child rather than asserting on a race with it.
func paneGone(t *testing.T, p *pane) paneGoneMsg {
	t.Helper()

	select {
	case err := <-p.done:
		return paneGoneMsg{err: err}
	case <-time.After(10 * time.Second):
		t.Fatal("the child's exit was never reported")
		return paneGoneMsg{}
	}
}

// The pane is a terminal, so the child should see the size it was given —
// nvim draws for whatever winsize answers, and a pane that lied about it
// draws nothing where it should.
func TestPaneGivesTheChildItsSize(t *testing.T) {
	t.Parallel()

	p, err := startPane(t.Context(), []string{"sh", "-c", "stty size"}, 97, 11)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	paneWait(t, p, "11 97")

	if msg := paneGone(t, p); msg.err != nil {
		t.Errorf("stty size exited with %v", msg.err)
	}
}

// Keys pressed on the pane reach the child, which is the whole point of the
// thing: a key swallowed by the screen instead is the failure this guards.
func TestPaneSendsKeysToTheChild(t *testing.T) {
	t.Parallel()

	p, err := startPane(t.Context(), []string{"cat"}, 60, 10)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	defer p.kill()

	p.send(tea.KeyPressMsg{Code: 'x', Text: "x"})

	paneWait(t, p, "x")
}

// A paste is text, not a burst of keypresses, and it is multi-byte more often
// than not: the emulator carries it to the child whole rather than dropping
// everything past the first byte.
func TestPaneSendsPasteToTheChild(t *testing.T) {
	t.Parallel()

	p, err := startPane(t.Context(), []string{"cat"}, 60, 10)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	defer p.kill()

	p.emu.Paste("héllo → wörld")

	paneWait(t, p, "héllo → wörld")
}

// A child that exits ends the hand-off, with its exit code where the code was
// not clean.
func TestPaneReportsTheExit(t *testing.T) {
	t.Parallel()

	ok, err := startPane(t.Context(), []string{"true"}, 40, 6)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	if msg := paneGone(t, ok); msg.err != nil {
		t.Errorf("a clean exit reported %v", msg.err)
	}

	bad, err := startPane(t.Context(), []string{"false"}, 40, 6)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	if msg := paneGone(t, bad); msg.err == nil {
		t.Error("a failed child read as a clean exit")
	}
}

// Killing a pane mid-run is how the screen leaves one it is done with: the
// pump has to see it end rather than block on a dead pty forever.
func TestPaneKillEndsTheWatch(t *testing.T) {
	t.Parallel()

	p, err := startPane(t.Context(), []string{"cat"}, 40, 6)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	p.kill()

	if msg := paneGone(t, p); msg.err == nil {
		t.Error("a killed child reported a clean exit")
	}
}

// The transcript is what a shell session leaves behind: the pane as it was
// rendered, so a redraw the terminal resolved never reaches the note.
func TestPaneTranscriptKeepsTheSession(t *testing.T) {
	t.Parallel()

	p, err := startPane(t.Context(), []string{"sh", "-c", "printf 'the-evidence\\r\\033[Kgone\\n'"}, 40, 6)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	if msg := paneGone(t, p); msg.err != nil {
		t.Fatalf("the session exited with %v", msg.err)
	}
	defer p.kill()

	if got := p.transcript(); !strings.Contains(got, "gone") || strings.Contains(got, "the-evidence") {
		t.Errorf("the transcript is not the rendered line: %q", got)
	}
}

// A resize reaches both sides of the pane: the emulator the screen draws, and
// the winsize the child would ask about.
func TestPaneResize(t *testing.T) {
	t.Parallel()

	p, err := startPane(t.Context(), []string{"cat"}, 40, 6)
	if err != nil {
		t.Fatalf("starting the pane: %v", err)
	}

	defer p.kill()

	p.resize(120, 9)

	if p.emu.Width() != 120 || p.emu.Height() != 9 {
		t.Errorf("the emulator is %dx%d after the resize", p.emu.Width(), p.emu.Height())
	}
}
