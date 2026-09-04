// Package artifact reads and writes the prepared review under .second-look/.
//
// The file is TOML because a person edits it. The agent that drafts comments and
// the gh CLI that posts them both speak JSON, so TOML is the middle and JSON is
// both edges.
//
// Every field is either posted or local, and the split is declared once here. A
// field absent from the schema is rejected on load rather than ignored, so a
// private note cannot reach GitHub by being forgotten.
package artifact

import (
	"errors"
	"fmt"
)

// SchemaVersion is the artifact format. Bump it when a field's meaning changes,
// never when one is added.
const SchemaVersion = 1

// Review is one prepared review. Fields tagged `post:"..."` reach GitHub; every
// other field stays on this laptop.
type Review struct {
	Version int    `json:"version" toml:"version"`
	Host    string `json:"host"    toml:"host"`
	Owner   string `json:"owner"   toml:"owner"`
	Repo    string `json:"repo"    toml:"repo"`
	Number  int    `json:"number"  toml:"number"`

	// HeadSHA is the commit the comments were written against. Posting against a
	// different head is refused, because an anchor that moved is an anchor that lies.
	HeadSHA string `json:"head_sha" post:"commit_id" toml:"head_sha"`

	// HeadRef and BaseRef are the branches the pull request joins. They are
	// recorded so a list of staged reviews can see a stack: a pull request
	// whose base is another one's head is read after it, not on its own.
	HeadRef string `json:"head_ref,omitempty" toml:"head_ref,omitempty"`
	BaseRef string `json:"base_ref,omitempty" toml:"base_ref,omitempty"`

	// Event is COMMENT, APPROVE, or REQUEST_CHANGES.
	Event string `json:"event" post:"event" toml:"event"`

	// Body is the review-level comment.
	Body string `json:"body" post:"body" toml:"body"`

	// Note is the top-level scratchpad: what was run, what could not be, what is
	// still unresolved. Shown in the TUI, never posted.
	Note string `json:"note" toml:"note"`

	Comments []Comment `json:"comments" toml:"comment"`
}

// Comment is one inline comment. Anchors follow GitHub's review-comment shape:
// Line is in the file's post-image when Side is RIGHT and its pre-image when LEFT.
type Comment struct {
	// ID is stable across edits so a hand-edit and an agent update refer to the
	// same comment. Local: GitHub assigns its own.
	ID string `json:"id" toml:"id"`

	Path      string `json:"path"                 post:"path"       toml:"path"`
	Line      int    `json:"line"                 post:"line"       toml:"line"`
	Side      string `json:"side"                 post:"side"       toml:"side"`
	StartLine int    `json:"start_line,omitempty" post:"start_line" toml:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty" post:"start_side" toml:"start_side,omitempty"`

	// Body is the exact text to post, and the only prose here that anyone else reads.
	Body string `json:"body" post:"body" toml:"body"`

	// Anchor is the diff line Line points at, quoted when the comment was
	// staged. Posting compares it against the live diff, so a comment whose
	// line moved is refused rather than landing on whatever now sits there.
	Anchor string `json:"anchor,omitempty" toml:"anchor,omitempty"`

	// InReplyTo is the review comment this answers. A reply posts through the
	// replies endpoint rather than inside the review payload.
	InReplyTo int64 `json:"in_reply_to,omitempty" toml:"in_reply_to,omitempty"`

	// Note is why this comment exists: the evidence, the command that proved it,
	// the doubt. Shown beside the comment in the TUI, never posted.
	Note string `json:"note" toml:"note"`

	// Severity ranks the comment in the TUI and orders what to read first.
	Severity string `json:"severity" toml:"severity"`

	// Status gates posting. A draft blocks the submit rather than posting or
	// vanishing, a todo blocks it because an agent still owes work here, and a
	// skip records a decision not to comment.
	Status string `json:"status" toml:"status"`

	// SkipReason explains a skip, so a declined finding reads as considered.
	SkipReason string `json:"skip_reason,omitempty" toml:"skip_reason,omitempty"`

	// Turns is the conversation about this comment, in the order it was said,
	// and it never posts. The note is one mutating field and loses the exchange
	// that produced it.
	Turns []Turn `json:"turn,omitempty" toml:"turn,omitempty"`
}

// Turn is one thing said about a comment, by me or by an agent. Author is free
// text because the agent naming itself is more useful than an enum this tool
// would have to keep current.
type Turn struct {
	Author string `json:"author" toml:"author"`
	Body   string `json:"body"   toml:"body"`
}

// Comment statuses.
const (
	StatusReady = "ready"
	StatusDraft = "draft"
	StatusSkip  = "skip"
	// StatusTodo means an agent owes work here. It is not a draft: a draft is a
	// comment nobody has ruled on, and a todo is one that has been ruled on and
	// handed back.
	StatusTodo = "todo"
)

// Review events.
const (
	EventComment        = "COMMENT"
	EventApprove        = "APPROVE"
	EventRequestChanges = "REQUEST_CHANGES"
)

// Comment sides.
const (
	SideRight = "RIGHT"
	SideLeft  = "LEFT"
)

var (
	validStatus = map[string]bool{StatusReady: true, StatusDraft: true, StatusSkip: true, StatusTodo: true}
	validEvent  = map[string]bool{EventComment: true, EventApprove: true, EventRequestChanges: true}
	validSide   = map[string]bool{SideRight: true, SideLeft: true}
	validSev    = map[string]bool{"blocker": true, "major": true, "minor": true, "nit": true, "question": true}
)

// Validate reports every problem at once, so a rejected batch tells the agent
// everything to fix rather than the first thing.
func (r *Review) Validate() error {
	errs := r.validateHeader()

	seen := make(map[string]bool, len(r.Comments))
	for i := range r.Comments {
		c := &r.Comments[i]

		where := name(c, i)

		if c.ID != "" && seen[c.ID] {
			errs = append(errs, fmt.Errorf("%s: %w", where, ErrDuplicateID))
		}
		seen[c.ID] = true

		errs = append(errs, c.validate(where)...)
	}

	return errors.Join(errs...)
}

func (r *Review) validateHeader() []error {
	var errs []error

	if r.Version != SchemaVersion {
		errs = append(errs, fmt.Errorf("%w: got %d, want %d", ErrVersion, r.Version, SchemaVersion))
	}
	if r.Owner == "" || r.Repo == "" || r.Number == 0 {
		errs = append(errs, ErrIdentity)
	}
	if r.Event != "" && !validEvent[r.Event] {
		errs = append(errs, fmt.Errorf("%w: %q", ErrEvent, r.Event))
	}

	return errs
}

func (c *Comment) validateTurns(where string) []error {
	var errs []error

	for i := range c.Turns {
		if c.Turns[i].Author == "" || c.Turns[i].Body == "" {
			errs = append(errs, fmt.Errorf("%s: turn %d: %w", where, i+1, ErrTurn))
		}
	}

	return errs
}

// validate checks one comment. The where argument names it in the message,
// because a comment with no id still has to be findable in the file.
func (c *Comment) validate(where string) []error {
	var errs []error

	if c.Body == "" && c.Status != StatusSkip {
		errs = append(errs, fmt.Errorf("%s: %w", where, ErrNoBody))
	}
	if !validStatus[c.Status] {
		errs = append(errs, fmt.Errorf("%s: %w: %q", where, ErrStatus, c.Status))
	}
	if c.Status == StatusSkip && c.SkipReason == "" {
		errs = append(errs, fmt.Errorf("%s: %w", where, ErrNoSkipReason))
	}
	if c.Severity != "" && !validSev[c.Severity] {
		errs = append(errs, fmt.Errorf("%s: %w: %q", where, ErrSeverity, c.Severity))
	}

	errs = append(errs, c.validateTurns(where)...)

	// A reply carries no anchor of its own: it lands under the comment it answers.
	if c.InReplyTo != 0 {
		return errs
	}

	if c.Path == "" {
		errs = append(errs, fmt.Errorf("%s: %w", where, ErrNoPath))
	}
	if c.Line <= 0 {
		errs = append(errs, fmt.Errorf("%s: %w", where, ErrLine))
	}
	if !validSide[c.Side] {
		errs = append(errs, fmt.Errorf("%s: %w: %q", where, ErrSide, c.Side))
	}
	if c.StartLine != 0 && c.StartLine > c.Line {
		errs = append(errs, fmt.Errorf("%s: %w: %d is after %d", where, ErrStartLine, c.StartLine, c.Line))
	}

	return errs
}
