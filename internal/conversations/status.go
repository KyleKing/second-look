package conversations

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Status is the last counts the queue computed, kept for a prompt that wants
// to know whether opening is worth it without paying for a fetch.
type Status struct {
	Updated time.Time `toml:"updated"`
	Unread  int       `toml:"unread"`
	Open    int       `toml:"open"`
}

// StatusPath is where the queue's last counts live, beside the read marks.
func StatusPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the config directory: %w", err)
	}

	return filepath.Join(dir, "second-look", "status.toml"), nil
}

// LoadStatus reads the last counts. A missing file is a queue nobody has run,
// not an error.
func LoadStatus(path string) (Status, error) {
	var out Status

	raw, err := os.ReadFile(path) //nolint:gosec // the user config directory plus a constant
	if os.IsNotExist(err) {
		return out, nil
	}

	if err != nil {
		return out, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := toml.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("reading %s: %w", path, err)
	}

	return out, nil
}

// SaveStatus writes the counts. What a prompt reads is as old as Updated says,
// which the command prints rather than pretending the numbers are live.
func SaveStatus(path string, s Status) error {
	body, err := toml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encoding the queue counts: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
