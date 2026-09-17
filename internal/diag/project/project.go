// Package project answers where in a checkout a tool should be run.
//
// A monorepo is several projects sharing a working tree, and a checker rooted
// at the whole of one reads a package's imports as unresolvable, applies the
// settings of a project the file does not belong to, and reports what the
// change did not cause. So a file names its own project, and the tool the
// project pins beats the one on the PATH, which is the same question an editor
// answers before it starts a language server.
package project

import (
	"os"
	"path/filepath"
	"strings"
)

// Root is the directory a tool is run in for one file: each group of markers is
// searched from the file up to the checkout in turn, and the checkout answers
// where none of them is found.
//
// The groups are tried before the directories on purpose. A go.mod beside the
// file and a go.work three directories above it both mark a root, and the
// workspace is the one that resolves the sibling modules the file imports, so
// depth is the wrong tiebreak between two markers that say different things.
//
// A directory holding a marker is skipped where it holds none of needs, which
// is what a tool cannot run without: rooted at a package carrying no typescript
// of its own, tsserver exits rather than answering.
func Root(checkout, path string, markers [][]string, needs []string) string {
	from := filepath.Dir(filepath.Join(checkout, path))

	for _, group := range markers {
		for dir := from; Inside(checkout, dir); dir = filepath.Dir(dir) {
			if marked(dir, group) && hosts(dir, needs) {
				return dir
			}
		}
	}

	return checkout
}

// Tool is the command to run, which is the project's own copy where it has one.
// A linter from the PATH is a different version under different settings than
// the one the project pins, and the two disagree about the same file.
//
// The bins are directories to look in, relative to a root, searched from the
// project up to the checkout so a workspace that installs once for all its
// packages is found too.
func Tool(checkout, root, name string, bins []string) string {
	if strings.ContainsRune(name, filepath.Separator) {
		return name
	}

	for dir := root; Inside(checkout, dir); dir = filepath.Dir(dir) {
		for _, bin := range bins {
			at := filepath.Join(dir, bin, name)
			if info, err := os.Stat(at); err == nil && !info.IsDir() {
				return at
			}
		}
	}

	return name
}

// Inside reports whether a directory is the checkout or under it. A prefix
// match is not the same question: /repo-two starts with /repo.
func Inside(checkout, dir string) bool {
	rel, err := filepath.Rel(checkout, dir)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func marked(dir string, group []string) bool {
	for _, marker := range group {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}

	return false
}

func hosts(dir string, needs []string) bool {
	for _, need := range needs {
		if _, err := os.Stat(filepath.Join(dir, need)); err != nil {
			return false
		}
	}

	return true
}
