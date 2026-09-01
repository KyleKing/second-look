// Package ghcassette records and replays gh subprocess calls. Every request
// second-look makes to GitHub goes through the gh binary, so the cassette sits
// where PATH resolves that binary rather than inside an HTTP transport: a test
// drives the real second-look binary, and the bytes it reads back are the ones
// GitHub actually sent.
//
// Cassettes are TOML but are named .golden, because hk's whitespace fixers
// exclude that suffix and nothing else, and a recorded diff whose trailing
// spaces were stripped on commit no longer matches the anchors it was recorded
// with.
package ghcassette

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/pelletier/go-toml/v2"
)

// ErrNoMatch reports a gh call the cassette has no recording for.
var ErrNoMatch = errors.New("no recorded gh interaction matches")

const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// Interaction is one gh invocation and everything it produced. The working
// directory is deliberately absent: it is a temporary path at record time and
// a different one on replay, so matching on it would make every cassette
// machine-specific.
//
//nolint:revive // multiline is go-toml's own tag option, which revive does not know
type Interaction struct {
	Args   []string `toml:"args"`
	Stdin  string   `toml:"stdin,multiline,omitempty"`
	Stdout string   `toml:"stdout,multiline,omitempty"`
	Stderr string   `toml:"stderr,multiline,omitempty"`
	Exit   int      `toml:"exit"`
}

// Cassette is an ordered log of gh invocations.
type Cassette struct {
	Interactions []Interaction `toml:"interaction"`
}

// Load reads a cassette from disk.
func Load(path string) (*Cassette, error) {
	raw, err := os.ReadFile(path) // #nosec G304,G703 -- a test fixture path
	if err != nil {
		return nil, fmt.Errorf("reading cassette %s: %w", path, err)
	}

	var c Cassette
	if err := toml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing cassette %s: %w", path, err)
	}

	return &c, nil
}

// Save writes a cassette, creating the directory it lives in.
func Save(path string, c *Cassette) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating cassette directory: %w", err)
	}

	raw, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding cassette: %w", err)
	}

	if err := os.WriteFile(path, raw, filePerm); err != nil { // #nosec G306 -- a test fixture
		return fmt.Errorf("writing cassette %s: %w", path, err)
	}

	return nil
}

// Response returns the stdout recorded for the first call matching args, so a
// test that needs the same bytes as a fixture reads them from the cassette
// rather than from a second copy on disk.
func (c *Cassette) Response(args ...string) (string, error) {
	i, err := c.Match(0, args)
	if err != nil {
		return "", err
	}

	return c.Interactions[i].Stdout, nil
}

// Match finds the first interaction at or after start whose args equal args.
// Replay is ordered rather than free lookup, so a call made twice with
// different responses replays both in the order they were recorded.
func (c *Cassette) Match(start int, args []string) (int, error) {
	for i := start; i < len(c.Interactions); i++ {
		if slices.Equal(c.Interactions[i].Args, args) {
			return i, nil
		}
	}

	return 0, fmt.Errorf("%w: gh %v", ErrNoMatch, args)
}
