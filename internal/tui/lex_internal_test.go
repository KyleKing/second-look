package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"
)

// longGo is a new Go file of n lines, long enough past lexAsyncLines that its
// grammar is read off the input goroutine.
func longGo(path string, n int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n",
		path, path, path, n)
	b.WriteString("+package pkg\n")

	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "+var v%d = \"a string\" // a comment\n", i)
	}

	return b.String()
}

// A long file's first frame draws without waiting on its grammar, the lex
// runs as a command, and its answer is what the next frame draws with.
func TestSpansFor_LongFileLexesBehindTheFrame(t *testing.T) {
	t.Parallel()

	const path = "pkg/long.go"

	m := model(t, longGo(path, 2*lexAsyncLines), nil)
	r := m.screen.rows[firstCode(t, m)]

	if spans := m.spansFor(r, r.line); spans != nil || !m.lexing[path] {
		t.Fatalf("before the lex lands: spans %v, lexing %v; want none, in flight", spans, m.lexing[path])
	}

	_ = m.Frame()

	if _, done := m.lexed[path]; done {
		t.Fatal("a frame lexed the long file where it was drawn")
	}

	m.Update(lexedMsg{diff: m.diff, path: path, spans: lexFile(m.diff, path)})

	if spans := m.spansFor(r, r.line); len(spans) == 0 || m.lexing[path] {
		t.Errorf("after the lex lands: spans %v, lexing %v; want the grammar's, done", spans, m.lexing[path])
	}
}

// The lex starts behind the frame from whichever Update moved the screen, once
// per file, and an answer for a diff the screen has since replaced is dropped.
func TestLexAhead_OncePerFileAndDropsAStaleAnswer(t *testing.T) {
	t.Parallel()

	const path = "pkg/long.go"

	m := model(t, longGo(path, 2*lexAsyncLines), nil)
	if !m.lexing[path] {
		t.Fatal("opening the screen did not start lexing the file it shows")
	}

	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	if m.lexAhead() != nil {
		t.Error("asking again started a lex already in flight")
	}

	old := m.diff
	m.diff = diff.Parse([]byte(longGo(path, 2*lexAsyncLines)))
	m.Update(lexedMsg{diff: old, path: path, spans: lexFile(old, path)})

	if _, kept := m.lexed[path]; kept {
		t.Error("kept a grammar read from a diff the screen no longer shows")
	}
}

// A short file is still lexed where it is drawn, so its first frame is never
// plain.
func TestSpansFor_ShortFileLexesInTheFrame(t *testing.T) {
	t.Parallel()

	m := model(t, onePatch, nil)
	r := m.screen.rows[firstCode(t, m)]

	if spans := m.spansFor(r, r.line); spans == nil {
		t.Error("a short file drew without its grammar")
	}
}

func firstCode(t *testing.T, m *Model) int {
	t.Helper()

	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.kind == rowCode && strings.TrimSpace(r.line.Text) != "" {
			return i
		}
	}

	t.Fatal("no code row")

	return -1
}
