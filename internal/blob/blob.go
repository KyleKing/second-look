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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
// another branch answers correctly anyway. A Work with no SHA reads the file
// off disk instead, which is the post-image a review of the checkout's own
// change is drawn from.
func (r Reader) Read(ctx context.Context, path string) ([]string, error) {
	if r.Work != "" && r.SHA == "" {
		return readDisk(r.Work, path)
	}

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

// Hit is the first line of one file carrying the text a Grep asked for.
type Hit struct {
	Path string
	Line int
}

// Grep is every file at the commit carrying text verbatim, each at its first
// line holding it. Only the checkout is asked: GitHub has no search scoped to
// one commit, so a Reader with no Work answers ErrNoSource.
func (r Reader) Grep(ctx context.Context, text string) ([]Hit, error) {
	if r.Work == "" {
		return nil, fmt.Errorf("searching for %q: %w", text, ErrNoSource)
	}

	args := []string{"grep", "-n", "-I", "-F", "--max-count=1", "-e", text}
	if r.SHA != "" {
		args = append(args, r.SHA)
	}

	cmd := exec.CommandContext(ctx, "git", append(args, "--")...) // #nosec G204 -- text is passed after -e
	cmd.Dir = r.Work

	out, err := cmd.Output()

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && len(exit.Stderr) == 0 {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("searching for %q: %w", text, err)
	}

	return hits(out, r.SHA), nil
}

// List is every file at the commit under dir, which an empty dir makes the
// whole tree. Like Grep, only the checkout is asked.
func (r Reader) List(ctx context.Context, dir string) ([]string, error) {
	if r.Work == "" {
		return nil, fmt.Errorf("listing %s: %w", dir, ErrNoSource)
	}

	if dir == "" {
		dir = "."
	}

	args := []string{"ls-files", "--", dir}
	if r.SHA != "" {
		args = []string{"ls-tree", "-r", "--name-only", r.SHA, "--", dir}
	}

	out, err := run(ctx, r.Work, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}

	return lines(out), nil
}

// hits reads git grep's path:line:text lines, which carry the commit first
// where one was searched.
func hits(out []byte, sha string) []Hit {
	var found []Hit

	for _, l := range lines(out) {
		if sha != "" {
			l = strings.TrimPrefix(l, sha+":")
		}

		path, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}

		number, _, _ := strings.Cut(rest, ":")
		if n, err := strconv.Atoi(number); err == nil {
			found = append(found, Hit{Path: path, Line: n})
		}
	}

	return found
}

// readDisk is the file as the working copy holds it, which is what a review of
// the checkout's own change draws its context from.
func readDisk(root, path string) ([]string, error) {
	if !fs.ValidPath(path) {
		return nil, fmt.Errorf("%s: %w", path, ErrNoSource)
	}

	out, err := os.ReadFile(filepath.Join(root, path)) //nolint:gosec // a path out of the diff, validated above
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
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
