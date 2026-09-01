package shellrun_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/shellrun"
)

// The transcript is what a person reads in a note, so what survives cleaning is
// the whole test: no escape sequences, no script(1) banner, no carriage
// returns, and nothing trailing.
func TestClean(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"banner", "Script started on Mon\nok\nScript done on Mon\n", "ok"},
		{"color", "\x1b[32mPASS\x1b[0m\n", "PASS"},
		{"osc title", "\x1b]0;a title\atail\n", "tail"},
		{"crlf", "one\r\ntwo\r\n", "one\ntwo"},
		{"trailing space", "ok   \n\n\n", "ok"},
		{"already clean", "go test ./...\nok\n", "go test ./...\nok"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := shellrun.Clean([]byte(tc.raw)); got != tc.want {
				t.Errorf("Clean(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// A build that ran for an hour ends in the part worth quoting, so the tail is
// what a capped transcript keeps, cut at a line so it does not open mid-word.
func TestCleanKeepsTheTail(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("a line of build output that says very little\n", 200) + "FAIL: the one that matters\n"

	got := shellrun.Clean([]byte(raw))
	if !strings.HasSuffix(got, "FAIL: the one that matters") {
		t.Errorf("the end was dropped: %q", got[max(0, len(got)-80):])
	}

	if !strings.HasPrefix(got, "…\n") {
		t.Error("a cut transcript does not say it was cut")
	}

	for _, line := range strings.Split(strings.TrimPrefix(got, "…\n"), "\n") {
		if line != "a line of build output that says very little" && line != "FAIL: the one that matters" {
			t.Errorf("the cut landed mid-line: %q", line)
		}
	}
}

// Capture is the only place that knows how the two script(1) flavors differ,
// and the proof is that a session actually reaches the file.
func TestCaptureRecordsTheSession(t *testing.T) {
	t.Parallel()

	transcript := filepath.Join(t.TempDir(), "typescript")

	cmd, err := shellrun.Capture(t.Context(), transcript, "echo", "the-evidence")
	if err != nil {
		t.Skipf("no script(1) here: %v", err)
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("running the capture: %v", err)
	}

	raw, err := os.ReadFile(transcript) //nolint:gosec // the test's own temp file
	if err != nil {
		t.Fatalf("reading the transcript: %v", err)
	}

	if got := shellrun.Clean(raw); !strings.Contains(got, "the-evidence") {
		t.Errorf("the transcript does not carry what ran: %q", got)
	}
}

// $SHELL is what the person chose; sh is what exists.
func TestShellFallsBackToSh(t *testing.T) {
	t.Setenv("SHELL", "")

	if got := shellrun.Shell(); got != "sh" {
		t.Errorf("Shell() = %q with $SHELL unset, want sh", got)
	}
}
