package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kyleking/second-look/internal/acp"
	"github.com/kyleking/second-look/internal/config"
)

// errBadAgentArg is an `agent` argument that is neither "check" nor a name.
var errBadAgentArg = errors.New("is not an agent name or a subcommand")

// agentCmd reports the configured agent and smoke-tests it. `second-look
// agent` names what T would run; `second-look agent check` runs the same
// start T takes — spawn, initialize, session, mode — so an argv, auth, or
// mode mistake shows up here rather than at the hand-off.
func agentCmd(ctx context.Context, args []string, stdout io.Writer) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	name, check := "", false

	for _, a := range args {
		switch {
		case a == "check":
			check = true
		case name == "":
			name = a
		default:
			return fmt.Errorf("%q %w", a, errBadAgentArg)
		}
	}

	if name == "" {
		name = os.Getenv("SECOND_LOOK_AGENT")
	}

	a := cfg.Picked(name)
	if a == nil {
		return fmt.Errorf("agent %q %w", name, errNoSuchAgent)
	}

	var out strings.Builder

	fmt.Fprintf(&out, "%s: %s\n", a.Name, strings.Join(a.Command, " "))

	if a.Mode != "" {
		fmt.Fprintf(&out, "mode: %s\n", a.Mode)
	}

	if check {
		if err := agentCheck(ctx, a, &out); err != nil {
			return err
		}
	}

	return write(stdout, out.String())
}

// agentCheck runs the agent far enough to prove the config: a session that
// opens, the modes it advertises, and what applying the configured mode said.
func agentCheck(ctx context.Context, a *config.Agent, out *strings.Builder) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("finding the working directory: %w", err)
	}

	s, err := acp.Start(ctx, a.Command, dir, "", a.Mode)
	if err != nil {
		return fmt.Errorf("agent %s: %w", a.Name, err)
	}
	defer s.Close()

	fmt.Fprintf(out, "session: %s over %s\n", s.ID(), s.Name())

	if modes := s.Modes(); len(modes) > 0 {
		ids := make([]string, len(modes))
		for i, m := range modes {
			ids[i] = m.ID
		}

		fmt.Fprintf(out, "modes: %s — running as %s\n", strings.Join(ids, ", "), s.Mode())
	}

	for _, e := range s.Entries() {
		if e.Kind == acp.Note {
			fmt.Fprintf(out, "note: %s\n", e.Text)
		}
	}

	fmt.Fprintln(out, "ok")

	return nil
}
