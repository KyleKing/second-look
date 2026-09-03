// Package generated answers whether a file was written by a machine.
//
// A generated file is evidence rather than a change: what matters is that it
// moved and by how much, and reading four hundred lines of it proves nothing a
// count does not. So the review orders it last, folds it, and says how big it
// was instead of showing it.
//
// The built-in list is what turns up in the repositories this tool is used on.
// No list will match a monorepo, which is why the config can name its own.
package generated

import (
	"path"
	"strings"
)

// Patterns are the built-in matches. Each is either a path suffix, a base name,
// or a directory name with a trailing slash, matched anywhere in the path.
//
// Globs are deliberately not the vocabulary: `*.pb.go` and `.pb.go` say the same
// thing here, and a suffix cannot be written wrong in a way that silently
// matches nothing.
var Patterns = []string{
	".min.css",
	".min.js",
	".pb.go",
	".snap",
	"Cargo.lock",
	"Gemfile.lock",
	"composer.lock",
	"go.sum",
	"node_modules/",
	"package-lock.json",
	"pnpm-lock.yaml",
	"poetry.lock",
	"uv.lock",
	"vendor/",
	"yarn.lock",
	"_generated.go",
	"_pb2.py",
	"__snapshots__/",
}

// Set is what counts as generated in one repository: the built-in patterns plus
// whatever the config added.
type Set struct {
	suffixes []string
	dirs     []string
}

// New builds the set. An empty extra list leaves the built-in patterns alone;
// the config adds to them rather than replacing them, because a monorepo's own
// generated tree does not stop a lockfile being one.
func New(extra []string) Set {
	var s Set

	for _, p := range append(append([]string{}, Patterns...), extra...) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if dir, cut := strings.CutSuffix(p, "/"); cut {
			s.dirs = append(s.dirs, dir)

			continue
		}

		s.suffixes = append(s.suffixes, p)
	}

	return s
}

// Match reports whether a path names a generated file. Matching is
// case-sensitive, the way every filesystem a diff comes from spells its paths.
func (s Set) Match(p string) bool {
	base := path.Base(p)

	for _, want := range s.suffixes {
		if base == want || strings.HasSuffix(p, want) {
			return true
		}
	}

	for _, dir := range s.dirs {
		if strings.HasPrefix(p, dir+"/") || strings.Contains(p, "/"+dir+"/") {
			return true
		}
	}

	return false
}
