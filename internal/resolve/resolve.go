// Package resolve marks a conversation dealt with.
//
// A thumbs-up is the standing marker: it is what a person leaves by hand, and on
// a pull request comment or a review body it is the only thing GitHub offers,
// since neither can be resolved. So every conversation gets the reaction, and a
// thread gets the resolve as well.
package resolve

import (
	"context"
	"errors"
	"fmt"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/ghrun"
)

// ErrNothingToResolve reports a conversation carrying neither a thread to
// resolve nor a comment to react to, which is a decoding fault rather than
// anything a person can act on.
var ErrNothingToResolve = errors.New("this conversation cannot be resolved or reacted to")

// resolveThread resolves one review thread.
const resolveThread = `mutation($id:ID!){
  resolveReviewThread(input:{threadId:$id}){thread{isResolved}}
}`

// thumbsUp is the marker for handled. It is a GraphQL mutation because REST
// reacts to an issue comment and to nothing else that appears here: a review
// body has no reactions endpoint at all.
const thumbsUp = `mutation($id:ID!){
  addReaction(input:{subjectId:$id,content:THUMBS_UP}){reaction{content}}
}`

// Run marks the conversation dealt with and reports what it did.
//
// The thread resolves first, because that is the half a reader is watching for
// and the half GitHub can refuse. A conversation already carrying the reaction
// is not reacted to again: addReaction refuses a duplicate, and a refusal on the
// second call would report a resolve that did land as a failure.
func Run(ctx context.Context, r ghrun.Runner, root string, c *conversations.Conversation) (string, error) {
	where := c.Where() + " " + c.Anchor()

	if c.ThreadID != "" {
		if err := r.Run(ctx, root, "api", "graphql", "-F", "id="+c.ThreadID, "-f", "query="+resolveThread); err != nil {
			return "", fmt.Errorf("resolving the thread on %s: %w", c.Where(), err)
		}

		if c.Handled {
			return "resolved " + where, nil
		}

		if err := react(ctx, r, root, c); err != nil {
			return "", fmt.Errorf("resolved %s, and the thumbs-up failed: %w", where, err)
		}

		return "resolved and thumbs-upped " + where, nil
	}

	if err := react(ctx, r, root, c); err != nil {
		return "", err
	}

	return "thumbs-upped " + where, nil
}

// react leaves the thumbs-up on the comment that opened the conversation, which
// is the point being acknowledged rather than the last word about it.
func react(ctx context.Context, r ghrun.Runner, root string, c *conversations.Conversation) error {
	id := c.First().NodeID
	if id == "" {
		return fmt.Errorf("%s: %w", c.Where(), ErrNothingToResolve)
	}

	if err := r.Run(ctx, root, "api", "graphql", "-F", "id="+id, "-f", "query="+thumbsUp); err != nil {
		return fmt.Errorf("reacting to %s: %w", c.Where(), err)
	}

	return nil
}
