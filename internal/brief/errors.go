package brief

import "errors"

// ErrNoComment reports an id the review does not carry.
var ErrNoComment = errors.New("no comment with that id is staged")
