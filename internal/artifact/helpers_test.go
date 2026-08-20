package artifact_test

import (
	"os"
	"testing"
)

func appendLine(t *testing.T, path, line string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}
