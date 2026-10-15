package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The openers and closers the languages below spell a one-line comment with.
const (
	slash = "// "
	hash  = "# "
	dash  = "-- "
	semi  = ";; "
	ml    = "<!-- "
	mr    = " -->"
)

// comment is how a file's own language says one line is prose: the opener a
// line comment needs and the closer a bracketed one takes. A file with no
// entry answers no marker, since a marker spelled in the wrong syntax is a
// line of dead text where a comment was meant.
var comment = map[string][2]string{
	".go": {slash, ""}, ".rs": {slash, ""}, ".java": {slash, ""},
	".js": {slash, ""}, ".jsx": {slash, ""}, ".ts": {slash, ""}, ".tsx": {slash, ""},
	".c": {slash, ""}, ".h": {slash, ""}, ".cc": {slash, ""}, ".cpp": {slash, ""}, ".hpp": {slash, ""},
	".cs": {slash, ""}, ".swift": {slash, ""}, ".kt": {slash, ""}, ".kts": {slash, ""},
	".scala": {slash, ""}, ".sc": {slash, ""}, ".php": {slash, ""}, ".dart": {slash, ""},
	".m": {slash, ""}, ".mm": {slash, ""}, ".fs": {slash, ""}, ".fsx": {slash, ""},

	".py": {hash, ""}, ".pyi": {hash, ""}, ".rb": {hash, ""}, ".pl": {hash, ""},
	".sh": {hash, ""}, ".bash": {hash, ""}, ".zsh": {hash, ""}, ".fish": {hash, ""},
	".r": {hash, ""}, ".yaml": {hash, ""}, ".yml": {hash, ""}, ".toml": {hash, ""},
	".tf": {hash, ""}, ".tfvars": {hash, ""}, ".ex": {hash, ""}, ".exs": {hash, ""},
	".nix": {hash, ""}, ".jl": {hash, ""}, ".ps1": {hash, ""}, ".ini": {hash, ""},
	".cfg": {hash, ""}, ".conf": {hash, ""}, ".coffee": {hash, ""}, ".cr": {hash, ""},

	".lua": {dash, ""}, ".sql": {dash, ""}, ".hs": {dash, ""}, ".elm": {dash, ""},
	".adb": {dash, ""}, ".ads": {dash, ""}, ".vhdl": {dash, ""},

	".vim": {"\" ", ""}, ".tex": {"% ", ""}, ".erl": {"% ", ""}, ".hrl": {"% ", ""},
	".clj": {semi, ""}, ".cljs": {semi, ""}, ".lisp": {semi, ""}, ".scm": {semi, ""},

	".html": {ml, mr}, ".xml": {ml, mr}, ".md": {ml, mr},
	".css": {"/* ", " */"}, ".vue": {ml, mr}, ".svg": {ml, mr},
}

// ErrNoCommentSyntax is what a file answers whose language has no entry in
// comment: binary, data, and the ones nobody here writes markers in.
var ErrNoCommentSyntax = errors.New("no comment syntax for this file")

// errEmpty and errNoLine name the two refusals WriteTODO can make without
// the file itself answering.
var (
	errEmpty  = errors.New("an empty marker says nothing")
	errNoLine = errors.New("no such line")
)

// WriteTODO lands a marker in the working copy: a line comment reading
// "[TODO: <note>]", indented like the line it guards and written above it.
// The marker is the whole of the feature's write side — once it is in the
// file the diff itself is the read side, and restaging shows it where it
// went.
func WriteTODO(root, rel string, at int, note string) error {
	open, end, ok := syntax(rel)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoCommentSyntax, filepath.Ext(rel))
	}

	// The note is one line in the file however it was typed.
	note = strings.Join(strings.Fields(note), " ")
	if note == "" {
		return errEmpty
	}

	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path) //nolint:gosec // a path under the review's own root
	if err != nil {
		return fmt.Errorf("reading %s: %w", rel, err)
	}

	lines := strings.Split(string(data), "\n")
	if at < 1 || at > len(lines) {
		return fmt.Errorf("%w: %s has %d lines, asked for %d", errNoLine, rel, len(lines), at)
	}

	marker := indentOf(lines[at-1]) + open + "[TODO: " + note + "]" + end
	lines = append(lines[:at-1], append([]string{marker}, lines[at-1:]...)...)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reading %s's mode: %w", rel, err)
	}

	//nolint:gosec // the same path the read above took, a file of the review's own tree
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm()); err != nil {
		return fmt.Errorf("writing %s: %w", rel, err)
	}

	return nil
}

// syntax is the line-comment shape the path's extension carries.
func syntax(rel string) (string, string, bool) {
	c, ok := comment[strings.ToLower(filepath.Ext(rel))]
	return c[0], c[1], ok
}

// indentOf is the whitespace a line opens with, which is where its marker
// belongs: under whatever the code is doing, at its own depth.
func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
