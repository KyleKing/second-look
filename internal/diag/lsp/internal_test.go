package lsp

import (
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
