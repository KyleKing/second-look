package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StateRoot is where a pull request's local state lives when its repository has
// no checkout on this machine.
//
// The artifact directory nests inside it exactly as it nests inside a checkout,
// so every path helper still takes a root and appends Dir, and none of them has
// to know which of the two it was handed.
func StateRoot(host, owner, name string) (string, error) {
	for _, part := range []string{host, owner, name} {
		if err := checkPart(part); err != nil {
			return "", err
		}
	}

	home, err := StateHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, host, owner, name), nil
}

// StateHome holds one directory per repository reviewed without a checkout. It
// is what lists them: a review nobody can find again is a review lost.
func StateHome() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the config directory: %w", err)
	}

	return filepath.Join(dir, "second-look"), nil
}

func checkPart(part string) error {
	if part == "" || part == "." || part == ".." ||
		strings.ContainsAny(part, `/\`) || strings.HasPrefix(part, ".") {
		return fmt.Errorf("%q: %w", part, ErrNotARepoName)
	}

	return nil
}
