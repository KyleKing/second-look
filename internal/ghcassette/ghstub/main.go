// Command ghstub stands in for the gh binary while tests run. It is placed on
// PATH ahead of the real gh, so every gh call second-look makes -- aragonite's
// pull request reads and second-look's own `gh api` posts alike -- goes
// through it.
//
// In record mode it runs the real gh and appends what happened to the
// cassette. In replay mode it answers from the cassette and fails loudly on a
// call nobody recorded, because a silent empty response would look like a
// pull request with no comments.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/ghcassette"
)

// exitHarness is returned for a failure of the stub itself, kept clear of the
// exit codes gh uses so a broken cassette is never read as a gh error.
const (
	exitHarness = 97
	journalPerm = 0o600
)

var (
	errNoCassette = errors.New("GH_CASSETTE is unset")
	errNoRealGH   = errors.New("GH_CASSETTE_REAL is unset, so record mode has no gh to run")
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghstub:", err)
		os.Exit(exitHarness)
	}

	os.Exit(code)
}

func run(args []string) (int, error) {
	path := os.Getenv("GH_CASSETTE")
	if path == "" {
		return 0, errNoCassette
	}

	stdin, err := readStdin(args)
	if err != nil {
		return 0, err
	}

	if os.Getenv("GH_CASSETTE_MODE") == "record" {
		return record(path, args, stdin)
	}

	return replay(path, args, stdin)
}

func replay(path string, args []string, stdin string) (int, error) {
	c, err := ghcassette.Load(path)
	if err != nil {
		return 0, fmt.Errorf("loading the cassette: %w", err)
	}

	journal := os.Getenv("GH_CASSETTE_JOURNAL")

	cursor, err := nextIndex(journal)
	if err != nil {
		return 0, err
	}

	i, err := c.Match(cursor, args)
	if err != nil {
		return 0, fmt.Errorf("%w\nstdin: %s", err, stdin)
	}

	if err := appendLine(journal, strconv.Itoa(i)); err != nil {
		return 0, err
	}

	it := c.Interactions[i]
	if _, err := os.Stdout.WriteString(it.Stdout); err != nil {
		return 0, fmt.Errorf("writing stdout: %w", err)
	}

	if _, err := os.Stderr.WriteString(it.Stderr); err != nil {
		return 0, fmt.Errorf("writing stderr: %w", err)
	}

	return it.Exit, nil
}

func record(path string, args []string, stdin string) (int, error) {
	ghPath := os.Getenv("GH_CASSETTE_REAL")
	if ghPath == "" {
		return 0, errNoRealGH
	}

	//nolint:gosec,noctx // the arguments are whatever the program under test passed to gh
	cmd := exec.Command(ghPath, args...)
	cmd.Stdin = strings.NewReader(stdin)

	var out, errOut strings.Builder

	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = io.MultiWriter(os.Stderr, &errOut)

	code := 0

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return 0, fmt.Errorf("running %s: %w", ghPath, err)
		}

		code = exitErr.ExitCode()
	}

	c, err := ghcassette.Load(path)
	if err != nil {
		c = &ghcassette.Cassette{}
	}

	c.Interactions = append(c.Interactions, ghcassette.Interaction{
		Args:   args,
		Stdin:  stdin,
		Stdout: out.String(),
		Stderr: errOut.String(),
		Exit:   code,
	})

	if err := ghcassette.Save(path, c); err != nil {
		return 0, fmt.Errorf("saving the cassette: %w", err)
	}

	return code, nil
}

// readStdin reads only when gh was told to read a body from it. Gh inherits
// whatever stdin the caller had otherwise, and draining a terminal would hang.
func readStdin(args []string) (string, error) {
	if !wantsStdin(args) {
		return "", nil
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}

	return string(raw), nil
}

func wantsStdin(args []string) bool {
	for i, a := range args {
		if a == "--input" && i+1 < len(args) && args[i+1] == "-" {
			return true
		}
	}

	return false
}

// nextIndex reads the journal of already-replayed interactions and returns the
// index replay resumes from.
func nextIndex(journal string) (int, error) {
	if journal == "" {
		return 0, nil
	}

	raw, err := os.ReadFile(journal) // #nosec G304,G703 -- a path the harness wrote
	if err != nil {
		return 0, nil //nolint:nilerr // an absent journal means nothing has replayed yet
	}

	lines := strings.Fields(string(raw))
	if len(lines) == 0 {
		return 0, nil
	}

	last, err := strconv.Atoi(lines[len(lines)-1])
	if err != nil {
		return 0, fmt.Errorf("reading journal %s: %w", journal, err)
	}

	return last + 1, nil
}

func appendLine(path, s string) error {
	if path == "" {
		return nil
	}

	// #nosec G304,G703 -- a path the harness wrote
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, journalPerm)
	if err != nil {
		return fmt.Errorf("opening journal %s: %w", path, err)
	}
	if _, err := fmt.Fprintln(f, s); err != nil {
		_ = f.Close() //nolint:errcheck // the write failure is the one worth reporting

		return fmt.Errorf("writing journal %s: %w", path, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("closing journal %s: %w", path, err)
	}

	return nil
}
