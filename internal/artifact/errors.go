package artifact

import "errors"

// Validation reasons. Each is a sentinel so a caller can match the reason and the
// message can still carry the offending value.
var (
	ErrAnchorMissing = errors.New("the line is not in the diff")
	ErrAnchorMoved   = errors.New("the diff line this comment anchors to has changed")
	ErrDuplicateID   = errors.New("duplicate id")
	ErrEvent         = errors.New("event is not one of COMMENT, APPROVE, REQUEST_CHANGES")
	ErrIdentity      = errors.New("owner, repo, and number are all required")
	ErrLine          = errors.New("line must be positive")
	ErrNoAnchor      = errors.New("no anchor was recorded; re-stage the comment against the current diff")
	ErrNoBody        = errors.New("body is required unless status is skip")
	ErrNoHeadSHA     = errors.New("the review has no head commit; run second-look get first")
	ErrNoPath        = errors.New("path is required")
	ErrNoSkipReason  = errors.New("a skip records why")
	ErrNotASHA       = errors.New("not a commit sha")
	ErrSeverity      = errors.New("severity is not one of blocker, major, minor, nit, question")
	ErrSide          = errors.New("side is not RIGHT or LEFT")
	ErrStartLine     = errors.New("start_line is after line")
	ErrStatus        = errors.New("status is not one of ready, draft, skip")
	ErrUnknownKey    = errors.New("the file carries a key the schema does not know")
	ErrVersion       = errors.New("unsupported schema version")
)
