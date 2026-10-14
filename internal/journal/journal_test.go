package journal_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/journal"
)

// One event is one line of JSON, which is the whole of the format: a sitting
// reads back with jq or a grep and nothing else.
func TestJournalWritesOneLinePerEvent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	j := journal.New(&buf)

	start := time.Now().Add(-150 * time.Millisecond)
	j.Key("queue", "j")
	j.Event("shell", "open")
	j.Event("review", "pane nvim")
	j.Since("review", "head", start)

	type line struct {
		T  string `json:"t"`
		S  string `json:"s"`
		E  string `json:"e"`
		W  string `json:"w"`
		MS int64  `json:"ms"`
	}

	var got []line

	for l := range strings.Lines(strings.TrimSpace(buf.String())) {
		var e line
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("line %q is not JSON: %v", l, err)
		}

		got = append(got, e)
	}

	if len(got) != 4 {
		t.Fatalf("journaled %d events, want 4", len(got))
	}

	if got[0].E != "key" || got[0].W != "j" || got[0].S != "queue" {
		t.Errorf("the key journaled as %+v", got[0])
	}

	if got[1].E != "open" {
		t.Errorf("the open journaled as %+v", got[1])
	}

	if got[2].E != "pane nvim" {
		t.Errorf("the pane's tenant came back %q", got[2].E)
	}

	if got[3].MS < 100 {
		t.Errorf("the wait measured %dms for an event 150ms old", got[3].MS)
	}
}

// A session whose journal could not open still works: every call on nil is a
// no-op rather than a panic, because the log may never cost a keystroke.
func TestNilJournalsSayNothing(t *testing.T) {
	t.Parallel()

	var j *journal.J
	j.Key("queue", "j")
	j.Event("shell", "open")
	j.Since("review", "head", time.Now())
}

// The journal lands under the state home, which os.UserConfigDir answers off
// HOME on every platform the test runs on.
//
// It cannot be parallel: it points HOME at a directory of its own.
func TestOpenAppendsUnderTheStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	j := journal.Open()
	if j == nil {
		t.Fatal("the journal did not open under a writable home")
	}

	j.Event("shell", "open")

	path, err := journal.Path()
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // a path this test just named
	if err != nil {
		t.Fatalf("nothing landed at %s: %v", path, err)
	}

	if !strings.Contains(string(body), `"e":"open"`) {
		t.Errorf("the file holds %q, not the event", body)
	}
}
