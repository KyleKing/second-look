// Package meta turns a lockfile's diff into a table of dependency changes, so
// a reviewer reads "12 packages updated, 1 added" instead of the hashes.
package meta

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
)

// Row is one dependency the lockfile changed. An empty From is an addition and
// an empty To is a removal.
type Row struct {
	Name string
	From string
	To   string
}

// Read turns a lockfile's diff into its dependency changes, in name order. It
// returns false for a file no format here recognizes.
func Read(f *diff.File) ([]Row, bool) {
	path := f.NewPath
	if path == "" {
		path = f.OldPath
	}

	var removed, added map[string]string

	switch filepath.Base(path) {
	case "go.sum":
		removed, added = goSum(f.Lines)
	case "go.mod":
		removed, added = goMod(f.Lines)
	case "Cargo.lock", "uv.lock":
		removed, added = tomlLock(f.Lines)
	case "package-lock.json":
		removed, added = npmLock(f.Lines)
	default:
		return nil, false
	}

	return pair(removed, added), true
}

// pair turns two name-to-version maps into rows, dropping a name whose
// version did not change.
func pair(removed, added map[string]string) []Row {
	names := make(map[string]struct{}, len(removed)+len(added))
	for name := range removed {
		names[name] = struct{}{}
	}

	for name := range added {
		names[name] = struct{}{}
	}

	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}

	slices.Sort(sorted)

	var rows []Row

	for _, name := range sorted {
		from, hasFrom := removed[name]
		to, hasTo := added[name]

		if hasFrom && hasTo && from == to {
			continue
		}

		rows = append(rows, Row{Name: name, From: from, To: to})
	}

	return rows
}

// goSum reads "<module> <version>[/go.mod] <hash>" lines, collapsing a
// module's /go.mod duplicate into the same entry.
func goSum(lines []diff.Line) (map[string]string, map[string]string) {
	removed, added := map[string]string{}, map[string]string{}

	for _, l := range lines {
		fields := strings.Fields(l.Text)

		const wantFields = 3
		if len(fields) < wantFields {
			continue
		}

		module := fields[0]
		version := strings.TrimSuffix(fields[1], "/go.mod")

		if l.Kind != diff.KindAdd {
			removed[module] = version
		}

		if l.Kind != diff.KindRemove {
			added[module] = version
		}
	}

	return removed, added
}

// goMod pairs each require line's module and version, tracking block state
// separately per side since a context line's side can disagree with a changed one's.
func goMod(lines []diff.Line) (map[string]string, map[string]string) {
	removed, added := map[string]string{}, map[string]string{}

	var inOld, inNew bool

	for _, l := range lines {
		text := strings.TrimSpace(l.Text)

		if l.Kind != diff.KindAdd {
			modLine(text, &inOld, removed)
		}

		if l.Kind != diff.KindRemove {
			modLine(text, &inNew, added)
		}
	}

	return removed, added
}

func modLine(text string, inBlock *bool, out map[string]string) {
	const (
		standaloneFields = 3
		blockFields      = 2
	)

	switch {
	case text == "require (":
		*inBlock = true
	case *inBlock && text == ")":
		*inBlock = false
	case *inBlock:
		if fields := strings.Fields(text); len(fields) >= blockFields {
			out[fields[0]] = fields[1]
		}
	case strings.HasPrefix(text, "require "):
		if fields := strings.Fields(text); len(fields) >= standaloneFields {
			out[fields[1]] = fields[2]
		}
	}
}

// tomlLock reads Cargo.lock and uv.lock's "[[package]]" blocks, pairing each
// version line with the most recent name line seen on the same side.
func tomlLock(lines []diff.Line) (map[string]string, map[string]string) {
	removed, added := map[string]string{}, map[string]string{}

	var lastOld, lastNew string

	for _, l := range lines {
		text := strings.TrimSpace(l.Text)

		if l.Kind != diff.KindAdd {
			tomlLine(text, &lastOld, removed)
		}

		if l.Kind != diff.KindRemove {
			tomlLine(text, &lastNew, added)
		}
	}

	return removed, added
}

func tomlLine(text string, lastName *string, out map[string]string) {
	switch {
	case strings.HasPrefix(text, "name = "):
		if v, ok := quoted(text, 0); ok {
			*lastName = v
		}
	case strings.HasPrefix(text, "version = "):
		if v, ok := quoted(text, 0); ok && *lastName != "" {
			out[*lastName] = v
		}
	}
}

const nodeModulesPrefix = "node_modules/"

// npmLock reads package-lock.json's "node_modules/<pkg>" keys, pairing each
// version line with the most recent such key seen on the same side.
func npmLock(lines []diff.Line) (map[string]string, map[string]string) {
	removed, added := map[string]string{}, map[string]string{}

	var lastOld, lastNew string

	for _, l := range lines {
		text := strings.TrimSpace(l.Text)

		if l.Kind != diff.KindAdd {
			npmLine(text, &lastOld, removed)
		}

		if l.Kind != diff.KindRemove {
			npmLine(text, &lastNew, added)
		}
	}

	return removed, added
}

func npmLine(text string, lastName *string, out map[string]string) {
	switch {
	case strings.HasPrefix(text, `"`+nodeModulesPrefix):
		if key, ok := quoted(text, 0); ok {
			*lastName = strings.TrimPrefix(key, nodeModulesPrefix)
		}
	case strings.HasPrefix(text, `"version":`):
		if v, ok := quoted(text, 1); ok && *lastName != "" {
			out[*lastName] = v
		}
	}
}

// quoted returns the nth (zero-indexed) quoted substring of s.
func quoted(s string, n int) (string, bool) {
	const quotesPerField = 2

	parts := strings.Split(s, `"`)

	idx := quotesPerField*n + 1
	if idx >= len(parts) {
		return "", false
	}

	return parts[idx], true
}
