// Package blob reads a file as it reads at the commit under review.
//
// A hunk carries three lines of context by convention and the question a review
// turns on is often in the fourth, so the screen has to be able to ask for more
// of the file. It asks the checkout when there is one, because that costs
// nothing and works with no network, and GitHub when there is not.
package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// shortSHA is the commit at the length a person reads, which is all an error
// message needs of it.
const shortSHA = 7

// ErrNoSource reports a file nothing could read: no checkout holds the commit
// and no repository was named to ask GitHub about.
var ErrNoSource = errors.New("nothing here can read that file")

// Reader reads one path at one commit.
type Reader struct {
	// Work is a checkout whose object store holds the commit, or empty.
	Work string
	// Repo is owner/name, and SHA the commit the review was written against.
	Repo string
	SHA  string
}

// Read is the file's lines, in order, with no trailing blank from the final
// newline. The checkout is asked first: it is free, it works offline, and `git
// show` reads the commit rather than the working tree, so a tree left on
// another branch answers correctly anyway.
func (r Reader) Read(ctx context.Context, path string) ([]string, error) {
	if r.SHA == "" {
		return nil, fmt.Errorf("%s: %w", path, ErrNoSource)
	}

	if r.Work != "" {
		if out, err := run(ctx, r.Work, "git", "show", r.SHA+":"+path); err == nil {
			return lines(out), nil
		}
	}

	if r.Repo == "" {
		return nil, fmt.Errorf("%s: %w", path, ErrNoSource)
	}

	out, err := run(ctx, ".", "gh", "api",
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", r.Repo, path, r.SHA),
		"-H", "Accept: application/vnd.github.raw")
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", path, r.SHA[:min(shortSHA, len(r.SHA))], err)
	}

	return lines(out), nil
}

func run(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- a path out of the diff and a commit
	cmd.Dir = dir

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running %s: %w: %s", bin, err, strings.TrimSpace(stderr.String()))
	}

	return out, nil
}

func lines(out []byte) []string {
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}
