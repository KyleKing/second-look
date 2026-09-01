package ghcassette_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/ghcassette"
)

// fakeGH echoes its arguments and exits with the code the caller asked for, so
// a record-then-replay round trip can be checked without touching GitHub.
func fakeGH(t *testing.T, exit int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-gh")
	script := "#!/bin/sh\necho \"args: $*\"\necho 'a warning' >&2\nexit " + strconv.Itoa(exit) + "\n"

	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // an executable is the point
		t.Fatalf("writing the fake gh: %v", err)
	}

	return path
}

func runGH(t *testing.T, s *ghcassette.Session, ghPath string, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), s.GH(), args...) // #nosec G204 -- the stub this package built
	cmd.Env = append(s.Env(t), "GH_CASSETTE_REAL="+ghPath)

	var out, errOut strings.Builder

	cmd.Stdout = &out
	cmd.Stderr = &errOut

	code := 0

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running the stub: %v (stderr: %s)", err, errOut.String())
		}

		code = exitErr.ExitCode()
	}

	return out.String(), errOut.String(), code
}

// TestRoundTrip records two gh calls against a fake binary, then replays them
// with a real path that does not exist, which is the guarantee the harness
// rests on: replay reaches nothing.
func TestRoundTrip(t *testing.T) {
	cassette := filepath.Join(t.TempDir(), "round-trip.toml")

	t.Setenv(ghcassette.RecordEnv, "1")

	rec := ghcassette.Start(t, cassette)
	if !rec.Recording() {
		t.Fatal("expected a recording session")
	}

	ghPath := fakeGH(t, 3)

	out, errOut, code := runGH(t, rec, ghPath, "pr", "view", "2")
	if out != "args: pr view 2\n" || errOut != "a warning\n" || code != 3 {
		t.Fatalf("record: out %q err %q exit %d", out, errOut, code)
	}

	if out, _, _ := runGH(t, rec, ghPath, "pr", "diff", "2"); out != "args: pr diff 2\n" {
		t.Fatalf("record: got %q", out)
	}

	t.Setenv(ghcassette.RecordEnv, "0")

	play := ghcassette.Start(t, cassette)

	out, errOut, code = runGH(t, play, "/nonexistent/gh", "pr", "view", "2")
	if out != "args: pr view 2\n" || errOut != "a warning\n" || code != 3 {
		t.Fatalf("replay: out %q err %q exit %d", out, errOut, code)
	}

	if out, _, _ := runGH(t, play, "/nonexistent/gh", "pr", "diff", "2"); out != "args: pr diff 2\n" {
		t.Fatalf("replay: got %q", out)
	}

	play.RequireAllPlayed(t)
}

// TestReplayRefusesUnrecordedCall pins the failure mode that matters: an
// unmatched call has to be loud, since an empty response would read as a pull
// request with no comments.
func TestReplayRefusesUnrecordedCall(t *testing.T) {
	t.Parallel()

	cassette := filepath.Join(t.TempDir(), "empty.toml")
	if err := ghcassette.Save(cassette, &ghcassette.Cassette{}); err != nil {
		t.Fatalf("saving: %v", err)
	}

	s := ghcassette.Start(t, cassette)

	_, errOut, code := runGH(t, s, "/nonexistent/gh", "pr", "view", "2")
	if code == 0 {
		t.Fatal("expected the stub to refuse an unrecorded call")
	}

	if !strings.Contains(errOut, "no recorded gh interaction matches") {
		t.Fatalf("want the refusal named, got %q", errOut)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()

	ghcassette.RemoveStub()
	os.Exit(code)
}
