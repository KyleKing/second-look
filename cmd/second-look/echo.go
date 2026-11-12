package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/kyleking/aragonite/forge/github"
)

// echo runs gh like aragonite's default runner but says each call as it is
// made and lets its stderr reach the terminal. A checkout can spend seconds
// fetching and moving a tree while writing nothing, and silence that long
// reads as a hang; the queue hands the terminal back for the work, so the
// output has somewhere safe to go. Stdout is not echoed: it carries answers
// the caller parses, and a diff or a JSON body is data, not progress.
func echo(w io.Writer) github.Runner {
	return func(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
		_, _ = fmt.Fprintf(w, "gh %s\n", ghCall(args)) //nolint:errcheck // progress, not correctness

		var out bytes.Buffer

		//nolint:gosec // gh's argv is built by the packages that name the call
		cmd := exec.CommandContext(ctx, "gh", args...)
		cmd.Dir = dir
		cmd.Stdout = &out
		cmd.Stderr = w
		if len(env) > 0 {
			cmd.Env = append(cmd.Environ(), env...)
		}

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("gh %s: %w", ghCall(args), err)
		}

		return out.Bytes(), nil
	}
}

// ghCall is the command line a reader recognizes, with payloads like a
// GraphQL query trimmed to what they are rather than what they contain.
func ghCall(args []string) string {
	const argCap = 60

	shown := make([]string, 0, len(args))

	for _, a := range args {
		if strings.HasPrefix(a, "query=") {
			a = "query=…"
		} else if len(a) > argCap {
			a = a[:argCap] + "…"
		}

		shown = append(shown, a)
	}

	return strings.Join(shown, " ")
}
