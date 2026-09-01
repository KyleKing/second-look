// Package ghrun runs one gh call behind an interface a test can replace.
//
// It is the seam for every mutation second-look sends that is not part of a
// review: resolving a thread, leaving a reaction, opening a pull request in a
// browser, merging one. The review itself posts through internal/post, which
// carries the anchor guard and the artifact cleanup a review needs and a
// one-shot call does not.
package ghrun

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs one gh call. The gh CLI is the only implementation that ships; a
// test supplies its own.
type Runner interface {
	Run(ctx context.Context, root string, args ...string) error
}

type ghRunner struct{}

// GH runs by shelling out to gh.
//
//nolint:ireturn // Runner is the seam a test replaces; concrete would remove it
func GH() Runner { return ghRunner{} }

// Run reports gh's own reason in the error rather than writing it to stderr.
// Every caller here is a key in a full-screen program, where a subprocess
// writing to the terminal draws over the frame and the reason belongs in the
// footer instead.
func (ghRunner) Run(ctx context.Context, root string, args ...string) error {
	//nolint:gosec // every argument is a constant or a value read off the pull request
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root

	if _, err := cmd.Output(); err != nil {
		// The first two arguments name the call ("api graphql"); the rest is a
		// whole mutation and belongs nowhere near a one-line failure.
		const named = 2

		return fmt.Errorf("gh %s: %w", strings.Join(args[:min(named, len(args))], " "), reason(err))
	}

	return nil
}

// reason unwraps gh's stderr, since (*exec.ExitError).Error() reports the exit
// status alone and cmd.Output leaves the message on the error rather than in it.
func reason(err error) error {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return err
	}

	if stderr := strings.TrimSpace(string(exit.Stderr)); stderr != "" {
		return fmt.Errorf("%w: %s", err, stderr)
	}

	return err
}
