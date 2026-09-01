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

// Tree is what the working directory holds for the pull request under review.
//
// A review needs no working copy: the diff, the threads, and the comment id a
// reply carries all come off the API. What a tree adds is reading around the
// change and running it, so this is what the shell key can use and the checkout
// key can offer.
type Tree int

const (
	// TreeOnHead is a checkout standing on the pull request head, where a shell
	// runs against the code the diff describes.
	TreeOnHead Tree = iota
	// TreeElsewhere is a checkout of the same repository standing on something
	// else, which C moves onto the pull request.
	TreeElsewhere
	// TreeNone is no checkout of the repository here. Nothing in the screen
	// conjures one: cloning is a decision rather than a side effect of reading.
	TreeNone
)

// WithTree says where the working copy stands. The default is a checkout on the
// head, which is what a review opened in its own repository has.
func WithTree(t Tree) Option {
	return func(m *Model) { m.tree = t }
}

// Outcome is what the screen was left through, beyond the failure it carries.
type Outcome struct {
	// Checkout reports that C asked for the working copy to be moved onto the
	// pull request. The screen closes first: the move asks about uncommitted
	// work, and two programs cannot own the terminal at once.
	Checkout bool
}

// Sender posts one comment on its own, outside any review. It is a separate
// seam from Submitter because it is a different request with a different
// consequence: the comment is gone from the prepared review afterwards, and the
// rest of the review is still staged.
type Sender func(ctx context.Context, r *artifact.Review, id string) (string, error)

// Merger merges the pull request under review. It is a separate seam from
// Submitter because it is a different consequence: a review can be taken back
// by deleting it on GitHub and a merge cannot.
type Merger func(ctx context.Context, r *artifact.Review) (string, error)

// WithMerger allows merging from inside the screen. Without one, the key says
// so rather than appearing to work.
func WithMerger(merge Merger) Option {
	return func(m *Model) { m.merge = merge }
}

// WithSender allows posting a single comment from inside the screen. Without
// one, the key says so rather than appearing to work.
func WithSender(send Sender) Option {
	return func(m *Model) { m.send = send }
}

// Opener shows the pull request in a browser. It reads nothing back: what the
// screen wants is for the page to be open, and whether anyone looked at it is
// not something a terminal can find out.
type Opener func(ctx context.Context, r *artifact.Review) error

// WithOpener allows o to open the pull request in a browser. Without one, the
// key says so rather than appearing to work.
func WithOpener(open Opener) Option {
	return func(m *Model) { m.browser = open }
}

// Run opens the review screen and blocks until the person leaves it. Every
// change is written to the artifact as it is made, so quitting loses nothing
// and a crash loses only the keystroke in flight.
//
// A submit that failed is returned once the screen has closed, since a footer
// the alternate screen takes back with it is not a report.
func Run(
	ctx context.Context, r *artifact.Review, d *diff.Diff,
	path string, submit Submitter, opts ...Option,
) (Outcome, error) {
	final, err := tea.NewProgram(New(ctx, r, d, path, submit, opts...)).Run()
	if err != nil {
		return Outcome{}, fmt.Errorf("running the review screen: %w", err)
	}

	if m, ok := final.(*Model); ok {
		return Outcome{Checkout: m.checkout}, m.failure
	}

	return Outcome{}, nil
}
