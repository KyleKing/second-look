// Package threads reads the review comments already on a pull request, so a
// second pass can see what was said last time and answer it.
//
// Nothing here is ever posted. A thread is context, and a reply to one is a
// comment staged in the prepared review with in_reply_to filled in, which goes
// out through the same path as every other comment.
package threads

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// Note is one comment inside a thread.
type Note struct {
	// ID is the REST database id, which is what in_reply_to takes. The GraphQL
	// node id names the same comment and the replies endpoint refuses it.
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

// Thread is one unresolved conversation anchored in the current diff.
type Thread struct {
	Path  string `json:"path"`
	Side  string `json:"side"`
	Line  int    `json:"line"`
	Notes []Note `json:"notes"`
}

// ReplyTo is the comment a reply to this thread addresses. GitHub threads a
// reply off the first comment, so answering the last one still lands here.
func (t *Thread) ReplyTo() int64 {
	if len(t.Notes) == 0 {
		return 0
	}

	return t.Notes[0].ID
}

// ForReply is a thread as a caller writing a reply needs it: where it anchors,
// what was said, and the one id in_reply_to takes. ReplyTo is computed rather
// than stored, so the cache never carries a value that could disagree with the
// notes beside it.
type ForReply struct {
	Thread

	ReplyTo int64 `json:"reply_to"`
}

// Replyable pairs each thread with the comment id that answers it.
func Replyable(ts []Thread) []ForReply {
	out := make([]ForReply, 0, len(ts))
	for i := range ts {
		out = append(out, ForReply{Thread: ts[i], ReplyTo: ts[i].ReplyTo()})
	}

	return out
}

// query asks for the threads and everything needed to place one: where it
// anchors, whether it is still live, and the database ids a reply needs.
const query = `query($owner:String!,$repo:String!,$number:Int!){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviewThreads(first:100){
        nodes{
          isResolved
          isOutdated
          path
          line
          diffSide
          comments(first:50){nodes{databaseId body author{login}}}
        }
      }
    }
  }
}`

// Fetch reads the pull request's unresolved, still-current review threads.
//
// Resolved and outdated threads are dropped: a second pass is about what is
// still open, and an outdated thread anchors to a line the diff no longer
// carries, so it has nowhere to render and nothing to answer.
func Fetch(ctx context.Context, root, owner, repo string, number int) ([]Thread, error) {
	//nolint:gosec // every argument is a constant or a value read off the pull request
	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-F", "owner="+owner, "-F", "repo="+repo, "-F", "number="+strconv.Itoa(number),
		"-f", "query="+query)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading the review threads on #%d: %w", number, ghError(err))
	}

	open, err := Decode(out)
	if err != nil {
		return nil, fmt.Errorf("on #%d: %w", number, err)
	}

	return open, nil
}

// ghError puts gh's own stderr in the message, which is where its reason for
// refusing lives; the exit status alone says nothing.
func ghError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, exitErr.Stderr)
	}

	return err
}
