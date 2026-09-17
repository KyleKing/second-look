package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The languageIds a didOpen carries for the built-in servers, which the
// protocol spells and a server matches on.
const (
	langJS = "javascript"
	langTS = "typescript"
)

// Server is one language server and the files it answers for.
type Server struct {
	// Name is what the notes are attributed to, so a reader can tell a type
	// error from a lint rule without tracing either.
	Name string
	// Argv is the command, already carrying whatever puts it on stdio.
	Argv []string
	// Exts are the file extensions it is asked about, with their dots.
	Exts []string
	// Language is the languageId a didOpen carries, per extension. A server
	// that is told nothing about a file's language treats it as plain text.
	Language map[string]string
	// Roots are the files that mark the project a server is started in, most
	// telling first, and the names inside one group rank equally. A server
	// rooted above the project compiles the file under settings that are not
	// the project's and reports errors the change did not cause, and a server
	// rooted below a workspace loses the sibling packages that workspace joins.
	Roots [][]string
	// Needs are paths, relative to a candidate root, without which the server
	// cannot run there: tsserver resolves its own typescript out of the
	// workspace and exits where there is none. A directory holding a marker and
	// none of these is walked past rather than started in.
	Needs []string
}

// builtin are the servers second-look starts without being configured to.
//
// It is a short list on purpose: each one here is a server this project has
// actually been run against, and a server named on a guess would announce a
// capability that does not work. Everything else is named in the config, which
// is the same seam the dispatch command uses.
var builtin = []Server{
	{
		Name: "tsserver",
		Argv: []string{"typescript-language-server", "--stdio"},
		Exts: []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"},
		Language: map[string]string{
			".ts": langTS, ".mts": langTS, ".cts": langTS,
			".tsx": "typescriptreact", ".jsx": "javascriptreact",
			".js": langJS, ".mjs": langJS, ".cjs": langJS,
		},
		Roots: [][]string{{"tsconfig.json", "jsconfig.json"}, {"package.json"}},
		Needs: []string{filepath.Join("node_modules", "typescript")},
	},
	{
		Name:     "gopls",
		Argv:     []string{"gopls"},
		Exts:     []string{".go"},
		Language: map[string]string{".go": "go"},
		Roots:    [][]string{{"go.work"}, {"go.mod"}},
	},
	{
		Name:     "pyright",
		Argv:     []string{"pyright-langserver", "--stdio"},
		Exts:     []string{".py", ".pyi"},
		Language: map[string]string{".py": "python", ".pyi": "python"},
		Roots:    [][]string{{"pyrightconfig.json"}, {"pyproject.toml", "setup.py", "setup.cfg"}},
	},
}

// Builtin is the default server list, which a config adds to or overrides by
// extension.
func Builtin() []Server { return append([]Server(nil), builtin...) }

// languageID is what a didOpen calls the file. An extension the server was
// listed for and has no name for is sent under the server's own name, which is
// what a single-language server expects anyway.
func (s Server) languageID(path string) string {
	if id, ok := s.Language[strings.ToLower(filepath.Ext(path))]; ok {
		return id
	}

	return s.Name
}

// rootFor is the directory a server is started in for one file: each group of
// markers is searched from the file up to the checkout in turn, and the
// checkout answers where none of them is found.
//
// The groups are tried before the directories on purpose. A go.mod beside the
// file and a go.work three directories above it both mark a root, and the
// workspace is the one that resolves the sibling modules the file imports, so
// depth is the wrong tiebreak between two markers that say different things.
func (s Server) rootFor(checkout, path string) string {
	from := filepath.Dir(filepath.Join(checkout, path))

	for _, group := range s.Roots {
		for dir := from; inside(checkout, dir); dir = filepath.Dir(dir) {
			if marked(dir, group) && hosts(dir, s.Needs) {
				return dir
			}
		}
	}

	return checkout
}

// hosts reports whether a server can run in a directory at all.
func hosts(dir string, needs []string) bool {
	for _, need := range needs {
		if _, err := os.Stat(filepath.Join(dir, need)); err != nil {
			return false
		}
	}

	return true
}

func marked(dir string, group []string) bool {
	for _, marker := range group {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}

	return false
}

// inside reports whether a directory is the checkout or under it. A prefix
// match is not the same question: /repo-two starts with /repo.
func inside(checkout, dir string) bool {
	rel, err := filepath.Rel(checkout, dir)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pick is the server for a path, and false where no configured server claims
// its extension or the one that does is not installed.
func pick(servers []Server, path string) (Server, bool) {
	ext := strings.ToLower(filepath.Ext(path))

	for _, s := range servers {
		if len(s.Argv) == 0 {
			continue
		}

		for _, e := range s.Exts {
			if !strings.EqualFold(e, ext) {
				continue
			}

			if _, err := exec.LookPath(s.Argv[0]); err != nil {
				return Server{}, false
			}

			return s, true
		}
	}

	return Server{}, false
}

// Answers reports whether any of these servers is installed and claims one of
// these paths, which is what decides whether a review offers the pass at all.
func Answers(servers []Server, paths []string) bool {
	for _, p := range paths {
		if _, ok := pick(servers, p); ok {
			return true
		}
	}

	return false
}
