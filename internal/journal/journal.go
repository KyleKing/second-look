// Package journal keeps the usage log the parked decisions in NEXT_STEPS.md
// are waiting on: one JSON line per key or event, so "which renderer did I
// reach for" is a grep rather than a memory.
package journal

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// J appends events to the journal. The zero J is a valid one — every method
// on nil is a no-op, which is what a screen gets when the file could not be
// opened: the log may never cost the session a keystroke.
type J struct {
	w   io.Writer
	now func() time.Time
}

// Entry is one line. E names what happened ("key", "open", "head"), W carries
// the detail when one is worth keeping (which key, which pane), and MS is the
// wait a Since call measured.
type entry struct {
	T  string `json:"t"`
	S  string `json:"s"`
	E  string `json:"e"`
	W  string `json:"w,omitempty"`
	MS int64  `json:"ms,omitempty"`
}

// Path is where the journal lives: one file for the tool rather than one per
// repository, since what it answers is about the person using it.
func Path() (string, error) {
	home, err := artifact.StateHome()
	if err != nil {
		return "", fmt.Errorf("finding the state home: %w", err)
	}

	return filepath.Join(home, "journal.jsonl"), nil
}

// Open appends to the journal at Path. A nil answer means nothing is
// recorded, which every caller should treat as fine rather than as a failure.
func Open() *J {
	path, err := Path()
	if err != nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil
	}

	//nolint:gosec // the state home plus a constant name
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		return nil
	}

	// The handle lives for the process: a journal closed early would drop the
	// keys that explain how the session ended.
	return New(f)
}

// New journals into w, which is what a test drives with a buffer.
func New(w io.Writer) *J { return &J{w: w, now: time.Now} }

// Key records a command keypress. Keys that went to text — a comment being
// written, a filter being typed, a child's own program — never reach here:
// the journal answers "what did I reach for", not "what did I write".
func (j *J) Key(scr, key string) { j.put(scr, "key", key, 0) }

// Event records something the session did: a review opened or left, a pane
// seated, a post. An event that wants a detail spells it in the name
// ("pane nvim"), since a schema field per kind is a taxonomy nobody reads.
func (j *J) Event(scr, ev string) { j.put(scr, ev, "", 0) }

// Since is Event carrying how long it took to arrive, which is the shape the
// wait questions take: how long the head check held the first frame, how long
// a review held the screen.
func (j *J) Since(scr, ev string, start time.Time) {
	if j == nil {
		return
	}

	j.put(scr, ev, "", j.now().Sub(start).Milliseconds())
}

func (j *J) put(scr, ev, what string, ms int64) {
	if j == nil || j.w == nil {
		return
	}

	line, err := json.Marshal(entry{
		T: j.now().Format(time.RFC3339),
		S: scr, E: ev, W: what, MS: ms,
	})
	if err != nil {
		return
	}

	//nolint:errcheck,gosec // a write that failed is a lost line, not a broken screen
	j.w.Write(append(line, '\n'))
}
