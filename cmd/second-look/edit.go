package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// errNothingWritten reports an editor left empty, which is how a person cancels.
var errNothingWritten = errors.New("nothing was written, so nothing was sent")

// editor is what $EDITOR names, or vi, which every system this runs on has.
func editor() string {
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}

	return "vi"
}

// edit opens $EDITOR on a temporary file and returns what came back, refusing
// an empty result rather than sending a blank comment.
//
// The review screen edits through Bubble Tea's own ExecProcess, which suspends
// the frame. This is for the list screens, which have already closed by the time
// it runs and can hand the terminal over directly.
func edit(ctx context.Context, seed string) (string, error) {
	file, err := os.CreateTemp("", "second-look-*.md")
	if err != nil {
		return "", fmt.Errorf("opening a temporary file: %w", err)
	}

	name := file.Name()
	defer os.Remove(name) //nolint:errcheck // a temp file that outlives the session is not worth an error path

	if _, err := file.WriteString(seed); err != nil {
		file.Close() //nolint:errcheck,gosec // the write already failed

		return "", fmt.Errorf("writing %s: %w", name, err)
	}

	if err := file.Close(); err != nil {
		return "", fmt.Errorf("closing %s: %w", name, err)
	}

	//nolint:gosec // the editor is the caller's own $EDITOR and the path is ours
	cmd := exec.CommandContext(ctx, editor(), name)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running %s: %w", editor(), err)
	}

	raw, err := os.ReadFile(name) //nolint:gosec // our own temp file
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", name, err)
	}

	body := strings.TrimSpace(string(raw))
	if body == "" {
		return "", errNothingWritten
	}

	return body, nil
}
