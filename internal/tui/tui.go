// Package tui is the review screen: the diff with the prepared review's
// comments inline, where a person reads a change and decides what to say about
// it before any of it reaches GitHub.
package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

// Run opens the review screen and blocks until the person leaves it. Every
// change is written to the artifact as it is made, so quitting loses nothing
// and a crash loses only the keystroke in flight.
// A submit that failed is returned once the screen has closed, since a footer
// the alternate screen takes back with it is not a report.
func Run(ctx context.Context, r *artifact.Review, d *diff.Diff, path string, submit Submitter) error {
	final, err := tea.NewProgram(New(ctx, r, d, path, submit)).Run()
	if err != nil {
		return fmt.Errorf("running the review screen: %w", err)
	}

	if m, ok := final.(*Model); ok {
		return m.failure
	}

	return nil
}
