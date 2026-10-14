package lsp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A shallow merge keeps only one side of a section both name: detected
// pythonPath and configured analysis settings both live under "python".
func TestMergeSettings(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"python":   map[string]any{"pythonPath": "/pkg/.venv/bin/python"},
		"detected": "yes",
	}
	over := map[string]any{
		"python":     map[string]any{"analysis": map[string]any{"typeCheckingMode": "strict"}},
		"configured": "yes",
	}

	want := map[string]any{
		"python": map[string]any{
			"pythonPath": "/pkg/.venv/bin/python",
			"analysis":   map[string]any{"typeCheckingMode": "strict"},
		},
		"detected":   "yes",
		"configured": "yes",
	}

	if got := mergeSettings(base, over); !reflect.DeepEqual(got, want) {
		t.Errorf("merged to %v, want %v", got, want)
	}

	if got := mergeSettings(nil, nil); got != nil {
		t.Errorf("nothing merged to %v, want nothing", got)
	}
}

// A workspace on TypeScript 6 has no lib/tsserver.js for
// typescript-language-server to exec, so the server is refused before a
// process is started rather than found out from one that dies.
func TestTsReady(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := tsReady(root); err != nil {
		t.Fatalf("a root with no typescript should pass: %v", err)
	}

	lib := filepath.Join(root, "node_modules", "typescript", "lib")
	if err := os.MkdirAll(lib, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := tsReady(root); err == nil {
		t.Fatal("a typescript with no lib/tsserver.js should be refused")
	}

	if err := os.WriteFile(filepath.Join(lib, "tsserver.js"), []byte("//"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tsReady(root); err != nil {
		t.Fatalf("a typescript that ships tsserver.js should pass: %v", err)
	}
}
