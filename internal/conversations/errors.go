package conversations

import "errors"

// ErrNoViewer reports a reply that named no viewer. Every bucketing decision
// turns on who you are, so a queue built without it would file every
// conversation as somebody else's.
var ErrNoViewer = errors.New("GitHub did not say who you are; check gh auth status")
