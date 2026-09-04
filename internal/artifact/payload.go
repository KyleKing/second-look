package artifact

import (
	"errors"
	"fmt"

	"github.com/kyleking/second-look/internal/humanize"
)

// reviewPayload is the body of POST /repos/{owner}/{repo}/pulls/{number}/reviews.
// Its fields are exactly the schema's `post` fields, spelled the way GitHub spells
// them, so the allowlist is this struct rather than a list that can drift from it.
type reviewPayload struct {
	CommitID string           `json:"commit_id"`
	Body     string           `json:"body,omitempty"`
	Event    string           `json:"event"`
	Comments []commentPayload `json:"comments,omitempty"`
}

type commentPayload struct {
	Path      string `json:"path"`
	Side      string `json:"side"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
	Line      int    `json:"line"`
	StartLine int    `json:"start_line,omitempty"`
}

// ReplyPayload is the body of POST /repos/{o}/{r}/pulls/{n}/comments/{id}/replies.
type ReplyPayload struct {
	Body      string `json:"body"`
	InReplyTo int64  `json:"-"`
}

// DraftError reports comments that are not ready. Posting stops rather than
// guessing whether an unfinished comment was meant to go out.
type DraftError struct{ Comments []Comment }

func (e *DraftError) Error() string {
	return humanize.Plural(len(e.Comments), "comment") +
		" still draft; each has to be ready or skipped before this posts"
}

// TodoError reports comments an agent still owes work on, which posting refuses
// for the same reason a draft does: it is unfinished.
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

		if c.Status != StatusReady {
			return nil, nil, fmt.Errorf("%s: %w", id, ErrNotPostable)
		}

		if c.InReplyTo != 0 {
			return ReplyPayload{Body: c.Body}, c, nil
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

// Find is the comment carrying an id, or nil.
func (r *Review) Find(id string) *Comment {
	for i := range r.Comments {
		if r.Comments[i].ID == id {
			return &r.Comments[i]
		}
	}

	return nil
}

// Payload builds what gets posted: the review itself, and the replies that have
// to go to their own endpoint. Skipped comments and every local field are absent
// by construction, since nothing here reads them.
func (r *Review) Payload() (any, []ReplyPayload, error) {
	if drafts := r.Drafts(); len(drafts) > 0 {
		return nil, nil, &DraftError{Comments: drafts}
	}

	if todos := r.Todos(); len(todos) > 0 {
		return nil, nil, &TodoError{Comments: todos}
	}

	if r.Empty() {
		return nil, nil, ErrNothingToPost
	}

	event := r.Event
	if event == "" {
		event = EventComment
	}

	out := reviewPayload{CommitID: r.HeadSHA, Body: r.Body, Event: event}

	var replies []ReplyPayload

	for i := range r.Comments {
		c := &r.Comments[i]
		if c.Status == StatusSkip {
			continue
		}
		if c.InReplyTo != 0 {
			replies = append(replies, ReplyPayload{InReplyTo: c.InReplyTo, Body: c.Body})

			continue
		}

		out.Comments = append(out.Comments, commentPayload{
			Path:      c.Path,
			Line:      c.Line,
			Side:      c.Side,
			StartLine: c.StartLine,
			StartSide: c.StartSide,
			Body:      c.Body,
		})
	}

	return out, replies, nil
}
