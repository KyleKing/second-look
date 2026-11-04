package artifact

import (
	"errors"
	"fmt"

	"github.com/kyleking/second-look/internal/humanize"
)

// commentPayload is the body of POST /repos/{owner}/{repo}/pulls/{number}/comments,
// the endpoint a single comment posts to on its own. Its fields are exactly the
// schema's `post` fields, spelled the way GitHub spells them, so the allowlist
// is this struct rather than a list that can drift from it.
type commentPayload struct {
	Path      string `json:"path"`
	Side      string `json:"side"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
	Line      int    `json:"line"`
	StartLine int    `json:"start_line,omitempty"`
}

// ReplyPayload is a reply as posting needs it: the text, the thread it
// answers, and whether the thread resolves once the reply lands.
type ReplyPayload struct {
	Body      string `json:"-"`
	InReplyTo int64  `json:"-"`
	// ThreadID is the GraphQL node id of the thread this answers, which is what
	// addPullRequestReviewThreadReply attaches to. It is empty only for a reply
	// staged by hand, since the screen always knows its thread.
	ThreadID string `json:"-"`
	// Resolve marks the reply that closes its thread once it is sent.
	Resolve bool `json:"-"`
	// ID is the staged comment's own id, for error messages and the record.
	ID string `json:"-"`
}

func replyOf(c *Comment) ReplyPayload {
	return ReplyPayload{
		InReplyTo: c.InReplyTo, Body: c.Body, ID: c.ID,
		ThreadID: c.ThreadID, Resolve: c.Resolve,
	}
}

// DraftError reports comments that are not ready. Posting stops rather than
// guessing whether an unfinished comment was meant to go out.
type DraftError struct{ Comments []Comment }

func (e *DraftError) Error() string {
	return humanize.Plural(len(e.Comments), "comment") +
		" still draft; each has to be ready or skipped before this posts"
}

// TodoError reports the review's own comments an agent still owes work on, which
// posting refuses for the same reason a draft does: it is unfinished.
type TodoError struct{ Comments []Comment }

func (e *TodoError) Error() string {
	return humanize.Plural(len(e.Comments), "comment") +
		" still todo; each has to be ready or skipped before this posts"
}

// ErrNothingToPost is a review that would reach GitHub carrying nothing: no
// body, no comment, and no reply. An approval says something on its own, so
// only a COMMENT review is refused this way.
var ErrNothingToPost = errors.New("this review has no body and no comments, so there is nothing to post")

// Empty reports whether posting would send an empty COMMENT review. The screen
// asks before it confirms, so the refusal arrives before the keystroke that
// would have sent it rather than after.
func (r *Review) Empty() bool {
	if r.Event != "" && r.Event != EventComment {
		return false
	}

	if r.Body != "" {
		return false
	}

	for i := range r.Comments {
		if r.Comments[i].Status != StatusSkip {
			return false
		}
	}

	return true
}

// ErrNoSuchComment is an id the prepared review does not carry.
var ErrNoSuchComment = errors.New("no comment with that id is staged")

// ErrNotPostable is a comment that cannot go out on its own: a skipped one was
// declined, and a draft has not been ruled on.
var ErrNotPostable = errors.New("a skipped or draft comment is not posted")

// OnePayload builds the body for a single comment posted on its own, outside a
// review. A reply carries only its text, since the endpoint it goes to already
// names the comment it answers.
func (r *Review) OnePayload(id string) (any, *Comment, error) {
	for i := range r.Comments {
		c := &r.Comments[i]
		if c.ID != id {
			continue
		}

		if c.Status != StatusReady && !c.TodoReply() {
			return nil, nil, fmt.Errorf("%s: %w", id, ErrNotPostable)
		}

		if c.InReplyTo != 0 {
			if c.Body == "" {
				return nil, nil, fmt.Errorf("%s: %w", id, ErrNoReplyText)
			}

			return replyOf(c), c, nil
		}

		return commentPayload{
			Path: c.Path, Side: c.Side, StartSide: c.StartSide,
			Body: c.Body, Line: c.Line, StartLine: c.StartLine,
		}, c, nil
	}

	return nil, nil, fmt.Errorf("%q: %w", id, ErrNoSuchComment)
}

// Remove drops a comment by id and reports whether it was there. It is what
// follows a comment posted on its own: GitHub owns it from that moment, and a
// copy left staged would go out a second time with the review.
func (r *Review) Remove(id string) bool {
	for i := range r.Comments {
		if r.Comments[i].ID == id {
			r.Comments = append(r.Comments[:i], r.Comments[i+1:]...)

			return true
		}
	}

	return false
}

// Only is a copy of the review holding just the comment carrying id, which is
// what a comment posted on its own hands off.
func (r *Review) Only(id string) *Review {
	one := *r
	one.Comments = nil

	if c := r.Find(id); c != nil {
		one.Comments = []Comment{*c}
	}

	return &one
}

// Find is the comment carrying an id, or nil.
func (r *Review) Find(id string) *Comment {
	for i := range r.Comments {
		if r.Comments[i].ID == id {
			return &r.Comments[i]
		}
	}

	return nil
}

// DraftThread is one comment as addPullRequestReview takes it. The field names
// are the input object's own, which unlike REST's keeps the side on the whole
// range rather than per end.
type DraftThread struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	StartSide string `json:"startSide,omitempty"`
	Body      string `json:"body"`
}

// Threads is what the review posts as: each comment as a draft thread for
// addPullRequestReview, and each reply for addPullRequestReviewThreadReply
// against the review it opens. Skipped comments and every local field are
// absent by construction, since nothing here reads them.
func (r *Review) Threads() ([]DraftThread, []ReplyPayload, error) {
	comments, replies, err := r.postable()
	if err != nil {
		return nil, nil, err
	}

	out := make([]DraftThread, 0, len(comments))

	for i := range comments {
		c := &comments[i]
		out = append(out, DraftThread{
			Path: c.Path, Line: c.Line, Side: c.Side,
			StartLine: c.StartLine, StartSide: c.StartSide, Body: c.Body,
		})
	}

	return out, replies, nil
}

// postable splits the review into what the review itself carries and what
// answers a thread, after the gates that refuse an unfinished review. A reply
// without a thread id is reported rather than dropped, because the GraphQL
// path attaches by thread and cannot guess one.
func (r *Review) postable() ([]Comment, []ReplyPayload, error) {
	if drafts := r.Drafts(); len(drafts) > 0 {
		return nil, nil, &DraftError{Comments: drafts}
	}

	if todos := r.todosWhere(false); len(todos) > 0 {
		return nil, nil, &TodoError{Comments: todos}
	}

	if r.Empty() {
		return nil, nil, ErrNothingToPost
	}

	var comments []Comment

	var replies []ReplyPayload

	for i := range r.Comments {
		c := &r.Comments[i]
		if c.Status == StatusSkip {
			continue
		}
		if c.InReplyTo != 0 {
			if c.Body == "" {
				return nil, nil, fmt.Errorf("%s: %w", c.ID, ErrNoReplyText)
			}

			replies = append(replies, replyOf(c))

			continue
		}

		comments = append(comments, *c)
	}

	return comments, replies, nil
}
