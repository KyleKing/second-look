package artifact

import "fmt"

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
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

// ReplyPayload is the body of POST /repos/{o}/{r}/pulls/comments/{id}/replies.
type ReplyPayload struct {
	InReplyTo int64  `json:"-"`
	Body      string `json:"body"`
}

// ErrDraft reports comments that are not ready. Posting stops rather than
// guessing whether an unfinished comment was meant to go out.
type ErrDraft struct{ Comments []Comment }

func (e *ErrDraft) Error() string {
	return fmt.Sprintf("%d comment(s) are still drafts; mark them ready or skip them", len(e.Comments))
}

// Payload builds what gets posted: the review itself, and the replies that have
// to go to their own endpoint. Skipped comments and every local field are absent
// by construction, since nothing here reads them.
func (r *Review) Payload() (any, []ReplyPayload, error) {
	if drafts := r.Drafts(); len(drafts) > 0 {
		return nil, nil, &ErrDraft{Comments: drafts}
	}

	event := r.Event
	if event == "" {
		event = EventComment
	}

	out := reviewPayload{CommitID: r.HeadSHA, Body: r.Body, Event: event}

	var replies []ReplyPayload
	for _, c := range r.Comments {
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
