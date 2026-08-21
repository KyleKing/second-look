package artifact_test

import (
	"encoding/json"
	"os"
	"testing"
)

func appendLine(t *testing.T, path, line string) {
	t.Helper()

	//nolint:gosec // path is a t.TempDir file this test just wrote
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()

	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(encoded)
}
