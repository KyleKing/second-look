// Command second-look prepares a code review locally and posts it in one call.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

// Set by goreleaser through -X main.<name>.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(exitCode())
}

// exitCode exists so the signal handler cleanup runs; os.Exit skips defers.
func exitCode() int {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			fmt.Printf("second-look %s (commit: %s, built: %s)\n", version, commit, date)

			return 0
		}
	}

	// A ctrl-c reaches gh through the context rather than leaving it to finish a
	// POST the user already abandoned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "second-look:", err)

		return 1
	}

	return 0
}
