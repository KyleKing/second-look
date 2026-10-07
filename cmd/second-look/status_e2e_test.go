package main_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/aragonite/ghcassette"
)

// The status line a prompt draws is the queue's last read plus what is staged,
// and a queue nobody has run says so rather than reporting an empty one.
func TestStatus(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, "main")

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "status", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))

	res := runCLI(t, s, dir, "status")
	if res.code != 0 {
		t.Fatalf("status: %s", res.stderr)
	}

	const fresh = "nothing new · nothing staged · the queue has not been read\n"
	if res.stdout != fresh {
		t.Fatalf("a queue nobody has read said %q, want %q", res.stdout, fresh)
	}

	home := testHome(t, dir)
	counts := "updated = '" + time.Now().Add(-3*time.Hour).UTC().Format(time.RFC3339) + "'\n" +
		"unread = 3\nopen = 5\n"
	write(t, filepath.Join(configDir(home), "second-look", "status.toml"), []byte(counts))
	seedReview(t, dir, sha)

	res = runCLI(t, s, dir, "status")
	if res.code != 0 {
		t.Fatalf("status: %s", res.stderr)
	}

	for _, want := range []string{"3 new conversations", "1 staged review", "1 blocked", "queue read 3h"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("the status line %q is missing %q", res.stdout, want)
		}
	}
}

// The command takes no arguments; one passed means the caller meant another.
func TestStatusTakesNoArguments(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "status-args", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))

	res := runCLI(t, s, t.TempDir(), "status", "extra")
	if res.code == 0 {
		t.Fatal("status extra ran to success")
	}
}

// configDir is where os.UserConfigDir looks under a home, which on macOS is
// the Library rather than .config.
func configDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support")
	}

	return filepath.Join(home, ".config")
}
