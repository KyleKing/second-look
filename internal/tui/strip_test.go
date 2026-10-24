package tui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/tui"
)

func TestShortestTail(t *testing.T) {
	t.Parallel()

	all := []string{
		"auth/index.ts", "ui/index.ts", "index.ts", "lib/util.ts",
		"a/x/index.go", "b/x/index.go", "c/y/index.go",
	}

	tests := []struct {
		path, want string
	}{
		{"auth/index.ts", "auth/index.ts"},
		{"ui/index.ts", "ui/index.ts"},
		{"index.ts", "index.ts"},
		{"lib/util.ts", "util.ts"},
		{"a/x/index.go", "a/x/index.go"},
		{"c/y/index.go", "y/index.go"},
	}

	for _, tc := range tests {
		if got := tui.ShortestTail(tc.path, all); got != tc.want {
			t.Errorf("%s is named %q, want %q", tc.path, got, tc.want)
		}
	}
}

func manyFiles(n int) string {
	var b strings.Builder

	for i := range n {
		dir := [...]string{"alpha", "beta", "gamma"}[i%3]
		fmt.Fprintf(&b, "diff --git a/%[1]s/f%[2]d.go b/%[1]s/f%[2]d.go\n--- a/%[1]s/f%[2]d.go\n"+
			"+++ b/%[1]s/f%[2]d.go\n@@ -1 +1 @@\n-old\n+new\n", dir, i)
	}

	return b.String()
}

func headerLine(m *tui.Model) string { return strings.Split(plain(m.Frame()), "\n")[1] }

// A file's segment is drawn heavy once its hunks are read, files inside one
// directory are told apart, and a diff too big for a segment each is drawn as
// its directories.
func TestTheStripFollowsWhatIsRead(t *testing.T) {
	t.Parallel()

	m := readable(t, patch)

	if got := headerLine(m); strings.Contains(got, "━") || !strings.Contains(got, "┊") {
		t.Errorf("an unread diff draws read segments, or no divider between its files: %q", got)
	}

	go2(t, m, ']', 'h')
	press(t, m, tea.KeyPressMsg{Code: ' ', Text: " "})

	if got := headerLine(m); !strings.Contains(got, "━") {
		t.Errorf("a file whose hunks are read is not drawn read: %q", got)
	}
}

// The strip keeps its segments where they are as the cursor walks a file, and
// its mark moves left to right through the lit one.
func TestTheStripHoldsStillAsTheCursorMoves(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{80, 24}, {200, 60}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()

			m := readable(t, patch)
			resize(m, size[0], size[1])

			var segments string

			mark := -1

			for range 4 {
				press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})

				line := []rune(headerLine(m))
				end := strings.LastIndexAny(string(line), "─━┊●")
				strip := []rune(string(line)[:end])
				at := slices.Index(strip, '●')

				if at < mark {
					t.Errorf("the mark moved back from %d to %d: %q", mark, at, string(line))
				}

				mark = at
				drawn := strings.ReplaceAll(string(strip), "●", "─")

				if segments != "" && drawn != segments {
					t.Errorf("the strip moved:\n%q\n%q", segments, drawn)
				}

				segments = drawn
			}

			if mark < 0 {
				t.Errorf("the strip draws no mark where the cursor stands: %q", headerLine(m))
			}
		})
	}
}

// A change small enough to tour opens on its chapters and p gives the files
// back; one too big for the tour opens on the files, drawn as directories once
// there are too many for a segment each.
func TestTheStripCollapsesToDirectories(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		n       int
		toured  bool
		divider bool
	}{{9, true, true}, {130, false, false}} {
		m, _, _ := fixtureWith(t, manyFiles(tc.n))
		m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})

		if line := headerLine(m); tc.toured && (strings.Contains(line, "┊") || !strings.Contains(line, "alpha")) {
			t.Errorf("%d files: the tour does not name its chapters: %q", tc.n, line)
		}

		if tc.toured {
			pressKey(t, m, 'p')
		}

		line := headerLine(m)
		if got := strings.Contains(line, "┊"); got != tc.divider {
			t.Errorf("%d files: divider between files = %v: %q", tc.n, got, line)
		}

		for _, dir := range []string{"alpha", "beta", "gamma"} {
			if !strings.Contains(line, dir) {
				t.Errorf("%d files: no label for %s: %q", tc.n, dir, line)
			}
		}
	}
}
