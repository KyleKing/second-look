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

import "fmt"

// SchemaVersion is the artifact format. Bump it when a field's meaning changes,
// never when one is added.
const SchemaVersion = 1

// Review is one prepared review. Fields tagged `post:"..."` reach GitHub; every
// other field stays on this laptop.
type Review struct {
	Version int    `toml:"version"`
	Host    string `toml:"host"`
	Owner   string `toml:"owner"`
	Repo    string `toml:"repo"`
	Number  int    `toml:"number"`

	// HeadSHA is the commit the comments were written against. Posting against a
	// different head is refused, because an anchor that moved is an anchor that lies.
	HeadSHA string `toml:"head_sha" post:"commit_id"`

	// Event is COMMENT, APPROVE, or REQUEST_CHANGES.
	Event string `toml:"event" post:"event"`

	// Body is the review-level comment.
	Body string `toml:"body" post:"body"`

	// Note is the top-level scratchpad: what was run, what could not be, what is
	// still unresolved. Shown in the TUI, never posted.
	Note string `toml:"note"`

	Comments []Comment `toml:"comment"`
}

// Comment is one inline comment. Anchors follow GitHub's review-comment shape:
// Line is in the file's post-image when Side is RIGHT and its pre-image when LEFT.
type Comment struct {
	// ID is stable across edits so a hand-edit and an agent update refer to the
	// same comment. Local: GitHub assigns its own.
	ID string `toml:"id" json:"id"`

	Path      string `toml:"path" json:"path" post:"path"`
	Line      int    `toml:"line" json:"line" post:"line"`
	Side      string `toml:"side" json:"side" post:"side"`
	StartLine int    `toml:"start_line,omitempty" json:"start_line,omitempty" post:"start_line"`
	StartSide string `toml:"start_side,omitempty" json:"start_side,omitempty" post:"start_side"`

	// Body is the exact text to post, and the only prose here that anyone else reads.
	Body string `toml:"body" json:"body" post:"body"`

	// InReplyTo is the review comment this answers. A reply posts through the
	// replies endpoint rather than inside the review payload.
	InReplyTo int64 `toml:"in_reply_to,omitempty" json:"in_reply_to,omitempty"`

	// Note is why this comment exists: the evidence, the command that proved it,
	// the doubt. Shown beside the comment in the TUI, never posted.
	Note string `toml:"note" json:"note"`

	// Severity ranks the comment in the TUI and orders what to read first.
	Severity string `toml:"severity" json:"severity"`

	// Status gates posting. A draft blocks the submit rather than posting or
	// vanishing, and a skip records a decision not to comment.
	Status string `toml:"status" json:"status"`

	// SkipReason explains a skip, so a declined finding reads as considered.
	SkipReason string `toml:"skip_reason,omitempty" json:"skip_reason,omitempty"`
}

// Comment statuses.
const (
	StatusReady = "ready"
	StatusDraft = "draft"
	StatusSkip  = "skip"
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
	validStatus = map[string]bool{StatusReady: true, StatusDraft: true, StatusSkip: true}
	validEvent  = map[string]bool{EventComment: true, EventApprove: true, EventRequestChanges: true}
	validSide   = map[string]bool{SideRight: true, SideLeft: true}
	validSev    = map[string]bool{"blocker": true, "major": true, "minor": true, "nit": true, "question": true}
)

// Validate reports every problem at once, so a rejected batch tells the agent
// everything to fix rather than the first thing.
func (r *Review) Validate() error {
	var errs []error

	if r.Version != SchemaVersion {
		errs = append(errs, fmt.Errorf("version is %d, want %d", r.Version, SchemaVersion))
	}
	if r.Owner == "" || r.Repo == "" || r.Number == 0 {
		errs = append(errs, fmt.Errorf("owner, repo, and number are all required"))
	}
	if r.Event != "" && !validEvent[r.Event] {
		errs = append(errs, fmt.Errorf("event %q is not one of COMMENT, APPROVE, REQUEST_CHANGES", r.Event))
	}

	seen := make(map[string]bool, len(r.Comments))
	for i := range r.Comments {
		c := &r.Comments[i]
		where := c.ID
		if where == "" {
			where = fmt.Sprintf("comment %d", i)
		}
		if c.ID != "" && seen[c.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate id", where))
		}
		seen[c.ID] = true

		if c.Body == "" && c.Status != StatusSkip {
			errs = append(errs, fmt.Errorf("%s: body is required unless status is skip", where))
		}
		if c.Status == "" || !validStatus[c.Status] {
			errs = append(errs, fmt.Errorf("%s: status %q is not one of ready, draft, skip", where, c.Status))
		}
		if c.Status == StatusSkip && c.SkipReason == "" {
			errs = append(errs, fmt.Errorf("%s: a skip records why", where))
		}
		if c.Severity != "" && !validSev[c.Severity] {
			errs = append(errs, fmt.Errorf("%s: severity %q is not one of blocker, major, minor, nit, question", where, c.Severity))
		}
		if c.InReplyTo == 0 {
			if c.Path == "" {
				errs = append(errs, fmt.Errorf("%s: path is required", where))
			}
			if c.Line <= 0 {
				errs = append(errs, fmt.Errorf("%s: line must be positive", where))
			}
			if !validSide[c.Side] {
				errs = append(errs, fmt.Errorf("%s: side %q is not RIGHT or LEFT", where, c.Side))
			}
			if c.StartLine != 0 && c.StartLine > c.Line {
				errs = append(errs, fmt.Errorf("%s: start_line %d is after line %d", where, c.StartLine, c.Line))
			}
		}
	}

	return joinErrs(errs)
}
