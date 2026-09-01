// Package shellrun hands the terminal to a shell and keeps what happened.
//
// It exists for one motion: run the code under review, then attach what it
// printed to the comment about it. A citation says where to look and a
// transcript says what actually happened, and only the second one survives a
// disagreement.
package shellrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// ErrNoScript reports that the script(1) utility is missing. It is what
// allocates the pty the shell needs while its output is being recorded, so
// there is no fallback: a shell whose output went to a pipe would not be
// interactive, and one attached to the real terminal would leave nothing to
// attach.
var ErrNoScript = errors.New("script(1) is not on PATH, so a shell session cannot be recorded")

// Shell is the shell to hand the terminal to, from $SHELL.
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}

	return "sh"
}

// Capture builds the command that runs argv with the terminal attached and the
// session written to transcript.
//
// The two script(1) flavors take their arguments in opposite orders, and only
// util-linux has --version, which is how they are told apart.
func Capture(ctx context.Context, transcript string, argv ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath("script")
	if err != nil {
		return nil, ErrNoScript
	}

	//nolint:gosec // argv is the user's own $SHELL and transcript is our temp file
	if utilLinux(ctx, path) {
		return exec.CommandContext(ctx, path, "-q", "-e", "-c", strings.Join(argv, " "), transcript), nil
	}

	//nolint:gosec // same
	return exec.CommandContext(ctx, path, append([]string{"-q", transcript}, argv...)...), nil
}

func utilLinux(ctx context.Context, path string) bool {
	out, err := exec.CommandContext(ctx, path, "--version").Output()

	return err == nil && strings.Contains(string(out), "util-linux")
}
