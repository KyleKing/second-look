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
	return fmt.Sprintf("%d comment(s) are still drafts; mark them ready or skip them", len(e.Comments))
}

// Payload builds what gets posted: the review itself, and the replies that have
// to go to their own endpoint. Skipped comments and every local field are absent
// by construction, since nothing here reads them.
func (r *Review) Payload() (any, []ReplyPayload, error) {
	if drafts := r.Drafts(); len(drafts) > 0 {
		return nil, nil, &DraftError{Comments: drafts}
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
