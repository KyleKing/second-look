// Package structure answers what a parser can see about a change, so a reader
// is not asked to look at a hunk that moved code without changing it.
//
// The grammars come from ast-grep rather than from a linked library. Every
// tree-sitter binding for Go needs cgo, and this project releases ten platforms
// with CGO_ENABLED=0, so linking one would build cleanly and ship nothing
// usable. Shelling out is the same bargain the tool already makes with gh, git,
// and $EDITOR: it works where the binary is installed, and every answer here
// has a text-only fallback for where it is not.
//
// Nothing here reads a working copy. A hunk carries both of its sides, so the
// structural pass runs on a review prepared with no checkout at all.
package structure

import (
	"os/exec"
	"path/filepath"
	"sync"
)

// grepBin is the structural tool. Its grammars are compiled in, which is why it
// is the one dependency here rather than one per language.
const grepBin = "ast-grep"

// Available reports whether the structural pass can run at all. A caller says
// so rather than pretending a text-only answer was a parsed one.
var Available = sync.OnceValue(func() bool {
	_, err := exec.LookPath(grepBin)

	return err == nil
})

// Lang is what one language is called and which of its node kinds matter.
//
// Kinds rather than patterns: a kind holds for every shape a language spells a
// declaration in, and a hunk is a fragment, where a pattern anchored to a whole
// file matches nothing.
type Lang struct {
	Name  string
	Decls []string
	Calls []string
}

// Node kinds shared by several grammars.
const (
	kindCall   = "call_expression"
	kindClass  = "class_declaration"
	kindFunc   = "function_declaration"
	kindFnDef  = "function_definition"
	kindMethod = "method_declaration"
)

var (
	ecmaDecls = []string{kindFunc, "method_definition", kindClass, "arrow_function"}
	ecmaCalls = []string{kindCall, "new_expression"}

	c   = Lang{Name: "c", Decls: []string{kindFnDef}, Calls: []string{kindCall}}
	cpp = Lang{
		Name:  "cpp",
		Decls: []string{kindFnDef, "class_specifier"},
		Calls: []string{kindCall},
	}
	csharp = Lang{
		Name:  "csharp",
		Decls: []string{kindMethod, kindClass},
		Calls: []string{"invocation_expression"},
	}
	golang = Lang{
		Name:  "go",
		Decls: []string{kindFunc, kindMethod, "type_declaration"},
		Calls: []string{kindCall},
	}
	java = Lang{
		Name:  "java",
		Decls: []string{kindMethod, kindClass},
		Calls: []string{"method_invocation"},
	}
	js  = Lang{Name: "javascript", Decls: ecmaDecls, Calls: ecmaCalls}
	php = Lang{
		Name:  "php",
		Decls: []string{kindFnDef, kindMethod, kindClass},
		Calls: []string{"function_call_expression", "member_call_expression"},
	}
	python = Lang{
		Name:  "python",
		Decls: []string{kindFnDef, "class_definition"},
		Calls: []string{"call"},
	}
	ruby = Lang{Name: "ruby", Decls: []string{"method", "class"}, Calls: []string{"call"}}
	rust = Lang{
		Name:  "rust",
		Decls: []string{"function_item", "impl_item", "struct_item"},
		Calls: []string{kindCall, "macro_invocation"},
	}
	ts  = Lang{Name: "typescript", Decls: ecmaDecls, Calls: ecmaCalls}
	tsx = Lang{Name: "tsx", Decls: ecmaDecls, Calls: ecmaCalls}
)

// langs maps an extension to its grammar. A language missing from here gets the
// text-only answer, which is why the list can grow one entry at a time.
var langs = map[string]Lang{
	".c":    c,
	".cc":   cpp,
	".cpp":  cpp,
	".cs":   csharp,
	".go":   golang,
	".java": java,
	".js":   js,
	".jsx":  js,
	".mjs":  js,
	".php":  php,
	".py":   python,
	".pyi":  python,
	".rb":   ruby,
	".rs":   rust,
	".ts":   ts,
	".tsx":  tsx,
}

// langFor is the grammar for a path, and whether there is one.
func langFor(path string) (Lang, bool) {
	l, ok := langs[filepath.Ext(path)]

	return l, ok
}
