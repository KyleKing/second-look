package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/second-look/internal/agents"
)

// The listing's contract is the shape `claude agents --json` prints: a
// background session carries state, an interactive one carries status, and a
// row naming no session is not one.
func TestListReadsTheSessionShape(t *testing.T) {
	t.Parallel()

	list := stub(t, `[
		{"id":"bfef1969","cwd":"/work/one","kind":"background","startedAt":1784694451272,
		 "sessionId":"bfef1969-ccdc-4165-aefd-1ddffb81e357","name":"Implement plan","state":"blocked"},
		{"pid":13639,"cwd":"/work/two","kind":"interactive","startedAt":1791410082371,
		 "sessionId":"502f4e3f-9e0a-4c6c-b443-533df62d8992","name":"watch","status":"busy"},
		{"id":"deadbeef","cwd":"/work/three","kind":"background","name":"no session id"}
	]`)

	live, err := agents.List(t.Context(), []string{list})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(live) != 2 {
		t.Fatalf("List returned %d sessions, want the two carrying ids: %v", len(live), live)
	}

	if got := live["bfef1969-ccdc-4165-aefd-1ddffb81e357"].State; got != agents.Blocked {
		t.Errorf("background session's state = %q, want blocked", got)
	}
	if got := live["502f4e3f-9e0a-4c6c-b443-533df62d8992"].State; got != "busy" {
		t.Errorf("interactive session's status = %q, want busy", got)
	}
	if live["bfef1969-ccdc-4165-aefd-1ddffb81e357"].Started.IsZero() {
		t.Error("startedAt did not parse")
	}
}

// A listing that fails, prints something else, or is never configured all
// arrive as an error the screen keeps quiet rather than a half-answer.
func TestListRefusesWhatIsNotAList(t *testing.T) {
	t.Parallel()

	if _, err := agents.List(t.Context(), nil); err == nil {
		t.Error("no argv listed fine")
	}

	if _, err := agents.List(t.Context(), []string{stub(t, `not json`)}); err == nil {
		t.Error("unreadable output listed fine")
	}

	missing := filepath.Join(t.TempDir(), "no-such-command")
	if _, err := agents.List(t.Context(), []string{missing}); err == nil {
		t.Error("a command that is not there listed fine")
	}
}

func stub(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "list.sh")
	script := "#!/bin/sh\nprintf '%s' '" + body + "'\n"

	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- the stub has to run
		t.Fatalf("writing the stub: %v", err)
	}

	return path
}
