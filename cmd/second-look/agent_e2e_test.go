package main_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
)

// agentStub builds the stand-in ACP agent once for the run: the screen talks
// to it the way it talks to `devin acp`, over a real subprocess's stdio.
func agentStub(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "agent-stub")

	//nolint:gosec // the only variable is the temp directory this test made
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin,
		"../../internal/acp/testdata/stub").CombinedOutput()
	if err != nil {
		t.Fatalf("building the stub agent: %v\n%s", err, out)
	}

	return bin
}

// Handing the todo set to a configured agent is a conversation, not a process
// that exits: T opens the session in the pane, the set goes over it as the
// prompt, and the ask the agent raises is answered where it was raised.
func TestReviewScreenDispatchesToAnAgentPane(t *testing.T) {
	t.Parallel()

	dir, sha := scratchRepo(t, headBranch)
	s := ghcassette.Replay(t, openCassette(t, sha))
	seedReview(t, dir, sha)

	// The config joins childEnv's own XDG root: a separate root moves the
	// artifact store too on Linux, and the staged review would vanish.
	write(t, filepath.Join(testHome(t, dir), ".config", "second-look", "config.toml"), []byte(fmt.Sprintf(`prefetch = 0

[[server]]
name = "none"
command = ["second-look-no-such-language-server"]
extensions = [".go", ".ts", ".py"]

[[agent]]
name = "stub"
command = [%q]
`, agentStub(t))))

	sc := openReview(t, s, dir, "2")
	sc.await("testdata/fixture/sample.go")

	sc.press("]c")
	sc.press("m")
	sc.press("t")
	sc.await("todo")

	sc.press("T")
	sc.await("the agent has the keyboard")
	sc.await("1 Allow")
	sc.press("1")
	sc.await("selected allow")

	// Esc leaves the pane, and the rows it covered repaint; sending q before
	// that repaint would pack \x1bq into alt+q and type into the prompt.
	mark := sc.mark()
	sc.press("\x1b")
	sc.awaitFrom(mark, "total := 0")
	sc.press("q")

	if code := sc.wait(); code != 0 {
		t.Fatalf("the screen exited %d:\n%s", code, sc.text())
	}

	review, err := artifact.Load(artifact.Path(stored(t, dir), 2))
	if err != nil {
		t.Fatalf("the prepared review: %v", err)
	}

	if review.Agent.Session != "stub-session" {
		t.Errorf("the review recorded %q, not the session it ran", review.Agent.Session)
	}

	s.RequireAllPlayed(t)
}
