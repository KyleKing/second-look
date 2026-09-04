package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

// s opens the line's own text and stages what comes back as a suggestion, which
// is the whole feature: typing three fences and the line's leading whitespace
// correctly is why nobody writes one from a terminal.
func TestSuggestOpensTheLineAndFencesTheAnswer(t *testing.T) {
	t.Parallel()

	m, path := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	onto(t, m, "lines, err := split(r)")
	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})

	frame := plain(m.Frame())

	if !strings.Contains(frame, "suggest a replacement for") {
		t.Fatalf("s did not open the editor:\n%s", frame)
	}

	if !strings.Contains(frame, "lines, err := split(r)") {
		t.Errorf("the editor did not open on the line's own text:\n%s", frame)
	}

	press(m, tea.KeyPressMsg{Code: '!', Text: "!"})
	press(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Comments) != 1 {
		t.Fatalf("%d comment(s) staged, want the suggestion", len(r.Comments))
	}

	text, ok := artifact.Suggestion(r.Comments[0].Body)
	if !ok {
		t.Fatalf("what was staged is not a suggestion: %q", r.Comments[0].Body)
	}

	if !strings.HasSuffix(text, "!") {
		t.Errorf("the suggestion is %q, want what was typed", text)
	}
}

// A removed line has no line in the file that results, so there is nothing for a
// suggestion to replace.
func TestSuggestRefusesARemovedLine(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	onto(t, m, "lines := split(r)")
	press(m, tea.KeyPressMsg{Code: 's', Text: "s"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "came out") {
		t.Errorf("a suggestion on a removed line was accepted:\n%s", frame)
	}
}

// A range crossing a line the post-image does not carry reads as fine and is
// refused on posting, so it is refused at staging instead.
func TestSuggestionOverAGapIsRefused(t *testing.T) {
	t.Parallel()

	c := artifact.Comment{
		ID: "s1", Path: parsed, Side: artifact.SideRight, StartLine: 14, Line: 18,
		Body: artifact.Suggest("replacement"), Status: artifact.StatusReady,
	}

	if err := artifact.CheckSuggestion(&c, diff.Parse([]byte(patch))); err == nil {
		t.Error("a suggestion covering a line the diff does not carry was accepted")
	}

	left := c
	left.StartLine, left.Line, left.Side = 0, 15, artifact.SideLeft

	if err := artifact.CheckSuggestion(&left, diff.Parse([]byte(patch))); err == nil {
		t.Error("a suggestion on the pre-image was accepted")
	}
}
