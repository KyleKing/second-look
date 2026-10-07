package main_test

import (
	"os"
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

// A configured listing runs fresh, and what it says of the sessions recorded
// on staged reviews joins the line: a blocked agent is the notification the
// line exists for. A session the listing does not name ended, so it counts
// for nothing.
func TestStatusCountsWhatTheListingSays(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, "main")
	home := quietHome(t)

	list := filepath.Join(home, "agents.sh")
	write(t, list, []byte("#!/bin/sh\nprintf '%s' '["+
		`{"sessionId":"sess-waiting","state":"blocked"},`+
		`{"sessionId":"sess-done","state":"done"},`+
		`{"sessionId":"sess-unstaged","state":"blocked"}`+
		"]'\n"))
	if err := os.Chmod(list, 0o700); err != nil { // #nosec G302 -- the stub has to run
		t.Fatalf("marking the stub listing executable: %v", err)
	}

	write(t, filepath.Join(home, ".config", "second-look", "config.toml"),
		[]byte("agents = [\""+list+"\"]\n"))

	root := storeFor(t, home, "KyleKing", "second-look")
	seedReviewAt(t, root, sha)
	seedReviewSessionAt(t, root, sha, "sess-waiting", 3)

	s := ghcassette.Replay(t, deriveFrom(t, "post-review", "status-agents", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))

	res := runCLIEnv(t, s, dir, homeEnv(home), "status")
	if res.code != 0 {
		t.Fatalf("status: %s", res.stderr)
	}

	if !strings.Contains(res.stdout, "1 agent waiting on you") {
		t.Errorf("the blocked session is missing from %q", res.stdout)
	}
	if strings.Contains(res.stdout, "sess-unstaged") || strings.Contains(res.stdout, "finished") {
		t.Errorf("sessions staged on no review leaked into %q", res.stdout)
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
