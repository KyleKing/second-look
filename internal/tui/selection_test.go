package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// A comment on more than one line could only be written by an agent into the
// TOML, because the screen had no way to say where a range ends. V says it: the
// range opens where it is pressed and closes wherever the cursor has reached.
func TestARangeCoversEveryLineItWasOpenedOn(t *testing.T) {
	t.Parallel()

	m, path := fixture(t)

	toCode(t, m)
	first := m.CursorAnchor()

	pressKey(t, m, 'V')
	pressKey(t, m, 'j')
	pressKey(t, m, 'j')

	last := m.CursorAnchor()

	pressKey(t, m, 'a')
	pressKey(t, m, 'm')
	typeIn(t, m, "these three lines want one comment")
	press(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	saved := reviewAt(t, path)
	if len(saved.Comments) != 1 {
		t.Fatalf("comments = %+v", saved.Comments)
	}

	c := saved.Comments[0]
	if c.StartLine != first || c.Line != last || c.StartSide != artifact.SideRight {
		t.Errorf("staged %d-%d on %q, want %d-%d on RIGHT", c.StartLine, c.Line, c.StartSide, first, last)
	}
}

// The range is answered by the key that uses it and by a second V, so the next
// comment is never quietly written against the last one's lines.
func TestARangeIsGoneOnceItIsUsedOrDropped(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		keys []rune
	}{
		{name: "a second V drops it", keys: []rune{'V', 'j', 'V'}},
		{name: "writing one takes it", keys: []rune{'V', 'j'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, path := fixture(t)

			toCode(t, m)

			for _, k := range tc.keys {
				pressKey(t, m, k)
			}

			if tc.name == "writing one takes it" {
				write(t, m, "the first covers a range")
				pressKey(t, m, 'j')
			}

			write(t, m, "and this one covers a line")

			last := reviewAt(t, path).Comments
			if got := last[len(last)-1]; got.StartLine != 0 || got.StartSide != "" {
				t.Errorf("a comment on one line carries start %d %q", got.StartLine, got.StartSide)
			}
		})
	}
}

// A suggestion over a range opens on every line it will replace, which is the
// whole of why nobody writes one by hand: the fences and each line's own
// leading whitespace, typed correctly, to say what an edit says by being it.
func TestASuggestedRangeOpensOnEveryLineItReplaces(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)

	toCode(t, m)
	pressKey(t, m, 'V')

	from := m.CursorAnchor()

	pressKey(t, m, 'j')
	pressKey(t, m, 'j')

	to := m.CursorAnchor()

	pressKey(t, m, 's')

	frame := plain(m.Frame())

	want := fmt.Sprintf("suggest a replacement for %s:%d-%d", parsed, from, to)
	if !strings.Contains(frame, want) {
		t.Fatalf("the editor is not opened on %q:\n%s", want, frame)
	}

	// Every line of the range is in the buffer, so what comes back replaces all
	// of them rather than the last.
	for _, line := range []string{"first := 1", "lines, err := split(r)"} {
		if strings.Count(frame, line) != 2 {
			t.Errorf("%q is not in both the diff and the buffer:\n%s", line, frame)
		}
	}
}

// toCode puts the cursor on the first line of the diff, which is where every
// range in these tests opens.
func toCode(tb testing.TB, m *tui.Model) {
	tb.Helper()

	go2(tb, m, ']', 'h')
	pressKey(tb, m, 'j')
}

// write stages a comment on whatever the cursor covers, ranked minor because
// the ranking is not what these tests are about.
func write(tb testing.TB, m *tui.Model, body string) {
	tb.Helper()

	pressKey(tb, m, 'a')
	pressKey(tb, m, 'n')
	typeIn(tb, m, body)
	press(tb, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
}
