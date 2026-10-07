// Package agents asks an agent tool which of its sessions are live.
//
// The listing is a configured command rather than a known one, for the same
// reason dispatch and resume are: the only thing this tool needs is the shape,
// and any tool that can print it fills the role. The shape is the one
// `claude agents --json` prints: a JSON array whose objects carry sessionId,
// which is the id `second-look session` records on a review, and state, which
// the screen repeats rather than interprets. Interactive sessions report
// status instead of state, and both are read.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Live is one session the listing reported. A session absent from the answer
// says nothing on its own: a foreground run ends there, a session nobody
// started in the background never appears, and a listing that does not cover
// finished runs reads the same as one that found none. The screen draws what
// is listed and nothing about what is not.
type Live struct {
	// Session is the tool's own resumable id, which is what a review records.
	Session string
	// State is what the session is doing, in the tool's words: blocked, busy,
	// idle, or done are Claude Code's. Keeping the word rather than mapping it
	// to an enum is what lets a second tool answer without this package
	// knowing its states first.
	State string
	// Name is what the tool calls the session, where a screen has room for it.
	Name string
	// Started is when the session began, where the tool says.
	Started time.Time
}

// The states a screen acts on rather than repeats. They are Claude Code's
// words; a tool listing different ones still draws them, it just does not mark
// them.
const (
	// Blocked is a session holding a question nobody has answered, which is
	// the one state that asks for a person.
	Blocked = "blocked"
	// Done is a run that finished, which is work waiting to be read.
	Done = "done"
)

//nolint:tagliatelle // the field names are the contract the configured command emits
type row struct {
	Session string `json:"sessionId"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Name    string `json:"name"`
	Started int64  `json:"startedAt"`
}

// errNoListing is an unset `agents` key, which callers check for before
// asking, so reaching it means the check and the use drifted.
var errNoListing = errors.New("no listing command is configured")

// List runs the configured command and answers the sessions it names, keyed by
// session id. A tool whose listing covers only background sessions answers
// only those; what it cannot see is the caller's word to keep, not this one.
func List(ctx context.Context, argv []string) (map[string]Live, error) {
	if len(argv) == 0 {
		return nil, errNoListing
	}

	//nolint:gosec // argv is the caller's own config
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("running %s: %w", argv[0], err)
	}

	var rows []row
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("reading what %s printed: %w", argv[0], err)
	}

	live := make(map[string]Live, len(rows))
	for _, r := range rows {
		if r.Session == "" {
			continue
		}

		state := r.State
		if state == "" {
			state = r.Status
		}

		one := Live{Session: r.Session, State: state, Name: r.Name}
		if r.Started > 0 {
			one.Started = time.UnixMilli(r.Started)
		}

		live[r.Session] = one
	}

	return live, nil
}
