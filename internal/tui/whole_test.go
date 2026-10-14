package tui_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// gf opens the whole file in $EDITOR, which is the escape hatch + and - are
// the cheap version of. The editor is a script that echoes its arguments and
// the file and then lingers, so the test sees the +N that puts the hunk's line
// under the cursor and the file's own lines the diff never carried.
func TestGFOpensTheFileInAPane(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\necho \"argv $@\"\ncat \"$2\"\nsleep 30\n"
	//nolint:gosec // a file the child runs has to be executable
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)

	m := withBlobs(t, func(context.Context, string) ([]string, error) {
		return []string{"the file's first line", "the part the diff never showed"}, nil
	})

	onto(t, m, "lines, err := split(r)")
	drive(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	drive(m, tea.KeyPressMsg{Code: 'f', Text: "f"})

	// The child draws when it draws, so the frame is polled rather than read
	// once: the pane's title is the file's name and its body holds what the
	// editor printed, which includes the line number it was opened at.
	deadline := time.Now().Add(10 * time.Second)
	for {
		frame := plain(m.Frame())
		if strings.Contains(frame, "the part the diff never showed") {
			if !strings.Contains(frame, "diff.go") {
				t.Fatalf("the pane is not named for the file:\n%s", frame)
			}
			if !strings.Contains(frame, "argv +15") {
				t.Errorf("the editor was not opened on the hunk's line:\n%s", frame)
			}

			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("the pane never drew the file:\n%s", frame)
		}

		time.Sleep(20 * time.Millisecond)
	}

	m.PaneClosed(nil)

	if frame := plain(m.Frame()); strings.Contains(frame, "the part the diff never showed") {
		t.Errorf("the pane stayed on screen after the child left:\n%s", frame)
	}
}
