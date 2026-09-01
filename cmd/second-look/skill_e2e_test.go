package main_test

import (
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/skill"
)

// TestSkillPrintsAnInstallableFile is what the command is for: what it writes
// has to be a complete skill file, since the documented use is redirecting it
// straight into a skills directory.
func TestSkillPrintsAnInstallableFile(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "no-calls", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))

	res := runCLI(t, s, t.TempDir(), "skill")
	if res.code != 0 {
		t.Fatalf("skill failed: %s", res.stderr)
	}

	if res.stdout != skill.Content {
		t.Error("the command printed something other than the file it embeds")
	}

	if !strings.HasPrefix(res.stdout, "---\nname: ") || !strings.Contains(res.stdout, "\ndescription: ") {
		t.Errorf("no frontmatter, so what it printed needs assembling:\n%s", firstLines(res.stdout))
	}

	// An agent that opens the review screen waits forever on a terminal nobody
	// is attached to, so the file has to say not to before anything else.
	if !strings.Contains(res.stdout, "Do not run `second-look <pr>`") {
		t.Error("the file does not warn an agent off the review screen")
	}
}

// TestSkillTakesNoArguments keeps a typo from being read as something to do.
func TestSkillTakesNoArguments(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, derive(t, "no-calls", func(c *ghcassette.Cassette) {
		c.Interactions = nil
	}))

	res := runCLI(t, s, t.TempDir(), "skill", "install")
	if res.code == 0 {
		t.Fatal("expected an argument to be refused")
	}

	if !strings.Contains(res.stderr, "usage: second-look skill") {
		t.Errorf("want the usage, got %q", res.stderr)
	}
}

func firstLines(s string) string {
	const n = 5

	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}

	return strings.Join(lines, "\n")
}
