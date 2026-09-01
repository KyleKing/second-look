// Package resolve marks a conversation dealt with.
//
// GitHub resolves an inline review thread and gives no equivalent for a pull
// request comment or a review body, so the two are marked the same way a person
// marks them by hand: a thumbs-up. The reaction and the resolve mean one thing
// here, which is why one key does both and this package chooses between them.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kyleking/second-look/internal/conversations"
)

// ErrNothingToResolve reports a conversation carrying neither a thread to
// resolve nor a comment to react to, which is a decoding fault rather than
// anything a person can act on.
var ErrNothingToResolve = errors.New("this conversation cannot be resolved or reacted to")

// Runner runs one gh call. The gh CLI is the only implementation that ships; a
// test supplies its own.
type Runner interface {
	Run(ctx context.Context, root string, args ...string) error
}

type ghRunner struct{}

// GH marks conversations by shelling out to gh.
//
//nolint:ireturn // Runner is the seam a test replaces; concrete would remove it
func GH() Runner { return ghRunner{} }

func (ghRunner) Run(ctx context.Context, root string, args ...string) error {
	//nolint:gosec // every argument is a constant or a value read off the pull request
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// The first two arguments name the call ("api graphql"); the rest is a
		// whole mutation and belongs nowhere near a one-line failure.
		const named = 2

		return fmt.Errorf("gh %s: %w", strings.Join(args[:min(named, len(args))], " "), err)
	}

	return nil
}

// resolveThread resolves one review thread.
const resolveThread = `mutation($id:ID!){
  resolveReviewThread(input:{threadId:$id}){thread{isResolved}}
}`

// react thumbs-ups whatever cannot be resolved. It is a GraphQL mutation
// because REST reacts to an issue comment and to nothing else that appears
// here: a review body has no reactions endpoint at all.
const react = `mutation($id:ID!){
  addReaction(input:{subjectId:$id,content:THUMBS_UP}){reaction{content}}
}`

// Run marks the conversation dealt with and reports what it did, so the caller
// can say which of the two happened rather than claiming one of them.
func Run(ctx context.Context, r Runner, root string, c *conversations.Conversation) (string, error) {
	if c.ThreadID != "" {
		if err := r.Run(ctx, root, "api", "graphql", "-F", "id="+c.ThreadID, "-f", "query="+resolveThread); err != nil {
			return "", fmt.Errorf("resolving the thread on %s: %w", c.Where(), err)
		}

		return "resolved " + c.Where() + " " + c.Anchor(), nil
	}

	id := c.Last().NodeID
	if id == "" {
		return "", fmt.Errorf("%s: %w", c.Where(), ErrNothingToResolve)
	}

	if err := r.Run(ctx, root, "api", "graphql", "-F", "id="+id, "-f", "query="+react); err != nil {
		return "", fmt.Errorf("reacting to %s: %w", c.Where(), err)
	}

	return "thumbs-upped " + c.Where() + " " + c.Anchor(), nil
}
