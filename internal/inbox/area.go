package inbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// areaFile is the pull request opened last from the queue and the directories it
// touches, which is what "same area" is compared against.
type areaFile struct {
	Pull string   `toml:"pull"`
	Dirs []string `toml:"dirs"`
}

// areaPath is beside the queue's read marks, under the user config directory.
func areaPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the config directory: %w", err)
	}

	return filepath.Join(dir, "second-look", "area.toml"), nil
}

// LoadArea reads the last opened pull request's directories. Any failure is an
// empty area: the order loses one tiebreak and works.
func LoadArea() map[string]bool {
	path, err := areaPath()
	if err != nil {
		return nil
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the user config directory plus a constant
	if err != nil {
		return nil
	}

	var f areaFile
	if err := toml.NewDecoder(strings.NewReader(string(raw))).Decode(&f); err != nil {
		return nil
	}

	out := make(map[string]bool, len(f.Dirs))
	for _, d := range f.Dirs {
		out[d] = true
	}

	return out
}

// SaveArea records the pull request just opened and its directories.
func SaveArea(pull string, dirs []string) error {
	path, err := areaPath()
	if err != nil {
		return err
	}

	body, err := toml.Marshal(areaFile{Pull: pull, Dirs: dirs})
	if err != nil {
		return fmt.Errorf("encoding the area: %w", err)
	}

	const dirPerm, filePerm = 0o750, 0o600

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, body, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
