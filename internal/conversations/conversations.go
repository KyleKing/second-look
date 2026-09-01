// Package conversations is the queue of open discussions across every pull
// request you are involved in: inline review threads, the pull request's own
// conversation comments, and the bodies submitted reviews carry.
//
// It exists because neither surface that already reports these answers the
// question. GitHub's notifications fire on everything and say nothing about who
// is waiting, and an issue tracker mirroring a pull request shows the newest
// comment rather than the thread it belongs to. What a reviewer needs is the
// discussions that moved since they last looked, and who owes the next word.
//
// Nothing here posts. Reading is this package's whole job; a reply is staged
// through the prepared review and a resolve goes out through internal/resolve.
package conversations

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Kind is which of the three surfaces a conversation lives on. It decides what
// answering one means: a thread takes a threaded reply and can be resolved, and
// the other two take a new comment on the pull request and cannot.
type Kind string

// The three surfaces.
const (
	KindThread  Kind = "thread"
	KindComment Kind = "comment"
	KindReview  Kind = "review"
)

// Note is one comment inside a conversation.
type Note struct {
	// ID is the REST database id, which is what in_reply_to takes. The GraphQL
	// node id names the same comment and the replies endpoint refuses it.
	ID int64 `json:"id"`
	// NodeID is the GraphQL id addReaction takes. A reaction is the only way to
	// mark a pull request comment or a review body dealt with, and no REST
	// endpoint reacts to a review body, so the node id is not optional.
	NodeID  string    `json:"node_id,omitempty"`
	Author  string    `json:"author"`
	Body    string    `json:"body"`
	Created time.Time `json:"created"`
}

// Conversation is one discussion that qualified for the queue.
type Conversation struct {
	Kind       Kind   `json:"kind"`
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`

	// ThreadID is the GraphQL node id resolveReviewThread takes. Only a thread
	// has one, which is why only a thread can be resolved.
	ThreadID string `json:"thread_id,omitempty"`

	// Path, Line, and Side place an inline thread in the diff. A conversation
	// comment anchors to the pull request rather than to a line and leaves them
	// empty.
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	Side string `json:"side,omitempty"`

	// Outdated marks a thread whose line the diff no longer carries. It stays in
	// the queue: a reply to an outdated thread is still owed, even though the
	// review screen has nowhere to draw it.
	Outdated bool `json:"outdated,omitempty"`

	// Handled is set when you have already thumbs-upped the conversation. The
	// reaction is the standing marker for dealt-with on all three surfaces, and
	// it is the only one the two GitHub gives no resolve can carry, so a handled
	// conversation leaves the queue the same way a resolved thread does.
	Handled bool `json:"handled,omitempty"`

	// Why is what qualified this conversation, kept so the screen can say why a
	// row is in front of you.
	Why Why `json:"why"`

	Notes []Note `json:"notes"`
}

// Why records every reason a conversation qualified. More than one is normal: a
// thread on your own pull request that you also replied in answers to both.
type Why struct {
	// Yours is set when the pull request is yours.
	Yours bool `json:"yours,omitempty"`
	// Spoke is set when you have a comment in this conversation.
	Spoke bool `json:"spoke,omitempty"`
	// Mentioned is set when a comment in it names you.
	Mentioned bool `json:"mentioned,omitempty"`
}

// Any reports whether anything qualified the conversation. A discussion that
// answers none of the three is somebody else's.
func (w Why) Any() bool { return w.Yours || w.Spoke || w.Mentioned }

// Key identifies a conversation across runs, so the last time you looked at one
// survives a refresh. A thread keeps GitHub's node id; the other two surfaces
// have no thread to name, so the comment that opens the discussion names it.
func (c *Conversation) Key() string {
	if c.ThreadID != "" {
		return c.ThreadID
	}

	return fmt.Sprintf("%s#%d:%s:%d", c.Repository, c.Number, c.Kind, c.firstID())
}

func (c *Conversation) firstID() int64 {
	if len(c.Notes) == 0 {
		return 0
	}

	return c.Notes[0].ID
}

// ReplyTo is the comment id a reply to this conversation addresses. GitHub
// threads a reply off the first comment, so answering the last one still lands
// here. It is zero on a surface that takes no threaded reply.
func (c *Conversation) ReplyTo() int64 {
	if c.Kind != KindThread {
		return 0
	}

	return c.firstID()
}

// First is the comment that opened the conversation, which is the one a
// thumbs-up marks: the point being acknowledged rather than the last word about
// it.
func (c *Conversation) First() Note {
	if len(c.Notes) == 0 {
		return Note{}
	}

	return c.Notes[0]
}

// Last is the most recent comment, which is the one that decides whose turn it
// is. A conversation with no comments never reaches the queue.
func (c *Conversation) Last() Note {
	if len(c.Notes) == 0 {
		return Note{}
	}

	return c.Notes[len(c.Notes)-1]
}

// Updated is when the conversation last moved.
func (c *Conversation) Updated() time.Time { return c.Last().Created }

// Where names the pull request a conversation belongs to.
func (c *Conversation) Where() string {
	return fmt.Sprintf("%s#%d", c.Repository, c.Number)
}

// Anchor is where the conversation sits, as a reader scans for it: a file and
// line for an inline thread, and the surface's own name for the other two.
func (c *Conversation) Anchor() string {
	switch {
	case c.Path != "" && c.Line > 0:
		return c.Path + ":" + strconv.Itoa(c.Line)
	case c.Path != "":
		return c.Path
	case c.Kind == KindReview:
		return "review body"
	default:
		return "conversation"
	}
}

// DefaultLimit is how many pull requests one fetch reads. The search is every
// open pull request you are involved in, which for an active month runs to
// dozens; the queue is read top-down by recency, so the tail is the part nobody
// reaches.
const DefaultLimit = 40

// Query is the search that finds the candidate pull requests. `involves`
// covers author, assignee, commenter, and mentioned in one term, which is every
// way a discussion becomes yours.
const Query = "is:pr is:open involves:@me"

// Queue is one read of the queue: who you are, and every conversation that
// qualified. The viewer travels with the conversations because every bucketing
// decision turns on it, and passing the two separately invites a queue bucketed
// against the wrong login.
type Queue struct {
	Viewer        string         `json:"viewer"`
	Conversations []Conversation `json:"conversations"`
}

// Fetch reads every qualifying conversation in one request.
//
// Viewer, search, threads, comments, and reviews arrive together because the
// bucketing needs all of them at once: whose turn it is cannot be decided
// without knowing who you are, and a per-pull-request round trip would spend a
// second each on pull requests with nothing in them.
func Fetch(ctx context.Context, root string, limit int) (*Queue, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}

	//nolint:gosec // the query is a constant and the limit is an integer
	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-F", "q="+Query, "-F", "n="+strconv.Itoa(limit), "-f", "query="+query)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading your open conversations: %w", ghError(err))
	}

	q, err := Decode(out)
	if err != nil {
		return nil, err
	}

	return q, nil
}

// ghError puts gh's own stderr in the message, which is where its reason for
// refusing lives; the exit status alone says nothing.
func ghError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}

	return err
}
