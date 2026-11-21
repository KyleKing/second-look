package agents_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

	want := map[string]agents.Live{
		"bfef1969-ccdc-4165-aefd-1ddffb81e357": {
			Session: "bfef1969-ccdc-4165-aefd-1ddffb81e357", State: agents.Blocked, Name: "Implement plan",
			Started: time.UnixMilli(1784694451272),
		},
		"502f4e3f-9e0a-4c6c-b443-533df62d8992": {
			Session: "502f4e3f-9e0a-4c6c-b443-533df62d8992", State: "busy", Name: "watch",
			Started: time.UnixMilli(1791410082371),
		},
	}

	if len(live) != len(want) {
		t.Fatalf("List returned %v, want the two sessions carrying ids", live)
	}

	for id, w := range want {
		if got := live[id]; got.Session != w.Session || got.State != w.State || got.Name != w.Name ||
			!got.Started.Equal(w.Started) {
			t.Errorf("session %s read as %+v, want %+v", id, got, w)
		}
	}

	// A listing that fails, prints something else, or is never configured all
	// arrive as an error the screen keeps quiet rather than a half-answer.
	for name, argv := range map[string][]string{
		"no argv":                 nil,
		"unreadable output":       {stub(t, `not json`)},
		"a command not installed": {filepath.Join(t.TempDir(), "no-such-command")},
	} {
		if got, err := agents.List(t.Context(), argv); err == nil {
			t.Errorf("%s listed %v", name, got)
		}
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
