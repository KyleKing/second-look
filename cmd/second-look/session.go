package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
)

var errUsageSession = errors.New("usage: second-look session <pr> [<session-id> [tool]]")

// sessionCmd records the agent session a review is being worked in, or prints
// the one already recorded.
//
// The agent writes its own id here rather than second-look reading one out of
// what a tool printed: every tool says it differently, and the one that knows
// the id for certain is the process holding it. Claude Code has it in
// CLAUDE_CODE_SESSION_ID; wavez prints its thread id under -json.
//
// It is recorded on the review, so it dies with the review: a round that posted
// leaves no session for the next one to resume against a diff that is gone.
func sessionCmd(ctx context.Context, args []string, stdout io.Writer) error {
	// The pull request, the id, and the tool that holds it.
	const most = 3

	if len(args) == 0 || len(args) > most {
		return errUsageSession
	}

	t, err := target(ctx, args[0])
	if err != nil {
		return err
	}

	path := artifact.Path(t.Store, t.Number)

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	if len(args) == 1 {
		if r.Agent.Session == "" {
			return write(stdout, "no session is recorded on this review\n")
		}

		return write(stdout, r.Agent.Session+" "+r.Agent.Tool+"\n")
	}

	r.Agent.Session = strings.TrimSpace(args[1])
	if len(args) == most {
		r.Agent.Tool = strings.TrimSpace(args[2])
	}

	if err := artifact.Save(path, r); err != nil {
		return fmt.Errorf("recording the session: %w", err)
	}

	return write(stdout, fmt.Sprintf("recorded %s on %s/%s #%d\n",
		r.Agent.Session, r.Owner, r.Repo, r.Number))
}
