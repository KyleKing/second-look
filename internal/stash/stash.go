// Package stash parks uncommitted work so the checkout can move onto another
// branch.
//
// Nothing here brings it back. Which branch the work belongs on is the person's
// call, and popping a stash onto a pull request's head is rarely what they
// meant.
package stash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Restore is what brings parked work back, quoted at people who have just had
// their tree emptied.
const Restore = "git stash pop"

// Push parks everything uncommitted, tracked and untracked alike, under label.
// A clean tree is not an error and parks nothing.
func Push(ctx context.Context, root, label string) error {
	//nolint:gosec // label is written here and root is the checkout being read
	cmd := exec.CommandContext(ctx, "git", "stash", "push", "--include-untracked", "--message", label)
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stashing your changes: %w", err)
	}

	return nil
}
