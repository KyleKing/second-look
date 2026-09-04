package tui_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// + grows the hunk with the file's own lines, which is what a hunk cut off in
// the middle of the thing being reviewed needs. The file is read once and the
// press that started the read is the press that gets the lines.
func TestMoreContextReadsTheFile(t *testing.T) {
	t.Parallel()

	var asked int

	file := make([]string, 40)
	for i := range file {
		file[i] = "line " + string(rune('a'+i%26))
	}

	file[12] = "the line above the hunk"

	m := withBlobs(t, func(context.Context, string) ([]string, error) {
		asked++

		return file, nil
	})

	onto(t, m, "lines, err := split(r)")

	press(m, tea.KeyPressMsg{Code: '+', Text: "+"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "the line above the hunk") {
		t.Fatalf("+ did not grow the hunk:\n%s", frame)
	}

	press(m, tea.KeyPressMsg{Code: '-', Text: "-"})

	if frame = plain(m.Frame()); strings.Contains(frame, "the line above the hunk") {
		t.Errorf("- did not shrink it back:\n%s", frame)
	}

	if asked != 1 {
		t.Errorf("the file was read %d times, want once and kept", asked)
	}
}

// Without a way to read the file the key says so rather than appearing to work.
func TestMoreContextWithNothingToReadSaysSo(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	onto(t, m, "lines, err := split(r)")
	press(m, tea.KeyPressMsg{Code: '+', Text: "+"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "no way to read") {
		t.Errorf("+ with no reader said:\n%s", frame)
	}
}

func withBlobs(t *testing.T, read tui.Blobs) *tui.Model {
	t.Helper()

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, (&counter{}).post,
		tui.WithBlobs(read))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	return m
}
