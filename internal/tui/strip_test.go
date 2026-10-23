package tui_test

import (
	"fmt"
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

func TestTheStripCollapsesToDirectories(t *testing.T) {
	t.Parallel()

	for _, n := range []int{9, 130} {
		m, _, _ := fixtureWith(t, manyFiles(n))
		m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})

		line := headerLine(m)
		if got := strings.Contains(line, "┊"); got != (n < 100) {
			t.Errorf("%d files: divider between files = %v: %q", n, got, line)
		}

		for _, dir := range []string{"alpha", "beta", "gamma"} {
			if !strings.Contains(line, dir) {
				t.Errorf("%d files: no label for %s: %q", n, dir, line)
			}
		}
	}
}
