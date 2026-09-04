// Package config reads the file that names the sections the inbox shows.
//
// The three built-in buckets answer one question well, and a queue is personal:
// which work is mine, which org's is worth watching, what a bot opened. So a
// section is a gh search query with a name, in the order they want doing, which
// is the shape gh-dash proved and the reason it is hard to leave.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Reasons a config is refused. A file that exists and says something wrong is
// worth stopping for: the alternative is a queue quietly showing the built-in
// buckets while the file it was written into is ignored.
var (
	ErrNoName     = errors.New("a section needs a name")
	ErrNoQuery    = errors.New("a section needs a query")
	ErrUnknownKey = errors.New("the file carries a key the schema does not know")
)

// Section is one query the inbox runs, under the name it is shown by.
type Section struct {
	Name  string `toml:"name"`
	Query string `toml:"query"`
}

// Config is the whole file.
type Config struct {
	// Limit is how many pull requests each section asks for. A queue longer
	// than this is not a queue, and the search costs the same either way.
	Limit int `toml:"limit,omitempty"`
	// Sections replace the built-in buckets outright when the file names any.
	// Merging the two would put rows in front of you that no query asked for.
	Sections []Section `toml:"section"`
	// Generated names files this repository writes by machine, added to the
	// built-in patterns rather than replacing them: a monorepo's own generated
	// tree does not stop a lockfile being one. A trailing slash means a
	// directory anywhere in the path; anything else is a path or name suffix.
	Generated []string `toml:"generated,omitempty"`
	// Prefetch is how many reviews ahead of the cursor the queue stages in the
	// background. Nil is the built-in default and zero turns it off, which is
	// the difference the pointer is for: a queue nobody works through would
	// otherwise spend an afternoon of API reads on rows nobody opened.
	Prefetch *int `toml:"prefetch,omitempty"`
	// Dispatch is the command T runs to hand the todo set to an agent, with the
	// file holding the set as its last argument. Unset, T writes the file and
	// names it, which is the safe default: running an agent is not something to
	// start on a keystroke nobody configured.
	Dispatch []string `toml:"dispatch,omitempty"`
}

// Path is where the config lives.
//
// It follows XDG rather than os.UserConfigDir, which on macOS answers
// ~/Library/Application Support: a file a person writes by hand and keeps in
// their dotfiles belongs beside gh's and gh-dash's, and the state second-look
// writes for itself is what belongs in the platform's own directory.
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "second-look", "config.toml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding your home directory: %w", err)
	}

	return filepath.Join(home, ".config", "second-look", "config.toml"), nil
}

// Load reads the config. A missing file is the empty config rather than an
// error: the built-in buckets are what most people want and nobody should have
// to write a file to get them.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is the caller's own config location
	if os.IsNotExist(err) {
		return &Config{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var c Config

	dec := toml.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&c); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("%s: %w\n%s", path, ErrUnknownKey, strict.String())
		}

		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &c, nil
}

func (c *Config) validate() error {
	for i := range c.Sections {
		s := &c.Sections[i]

		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("section %d: %w", i+1, ErrNoName)
		}

		if strings.TrimSpace(s.Query) == "" {
			return fmt.Errorf("section %q: %w", s.Name, ErrNoQuery)
		}
	}

	return nil
}
