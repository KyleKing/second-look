package tui_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// errNoServer is a pass that could not run at all, which is a laptop missing
// the server the review needs.
var errNoServer = errors.New("gopls is not installed")

// prober stands in for a language server and the configured checks. The
// protocol itself is covered against a real server in internal/diag/lsp; what
// is worth driving here is what the screen does with the answers.
type prober struct {
	notes  []diag.Note
	syms   []diag.Symbol
	err    error
	closed bool
}

func (p *prober) Notes(context.Context) ([]diag.Note, error) { return p.notes, p.err }

func (p *prober) Hover(context.Context, string, int) ([]diag.Symbol, error) {
	return p.syms, p.err
}

func (p *prober) Close() { p.closed = true }

func found() *prober {
	return &prober{
		notes: []diag.Note{
			{
				Path: parsed, Line: 15, Source: "typescript", Code: "2339",
				Message:  "Property 'is_archived' does not exist on type 'Row'.",
				Severity: diag.Error,
			},
			{
				Path: parsed, Line: 16, Source: "house rules", Code: "long-comment",
				Message: "a comment block runs past two lines", Severity: diag.Warning,
			},
			{
				Path: parsed, Line: 14, Source: "house rules", Code: "naming",
				Message: "on a line this change only carried", Severity: diag.Warning,
			},
		},
		syms: []diag.Symbol{
			{Name: "split", Text: "function split(r: io.Reader): [string[], error]"},
		},
	}
}

// checked is the review with a pass already answered, which is the state the
// screen is in a second or two after it opens.
func checked(t *testing.T, p tui.Prober) *tui.Model {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pr-42.toml")

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, (&counter{}).post,
		tui.WithProber(p))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Checked()

	return m
}

// A note goes under the line it is about, because the message is the whole of
// it: a mark in the margin says a line is wrong and leaves the reader to go
// somewhere else to find out how.
func TestNotesAreDrawnUnderTheirLine(t *testing.T) {
	t.Parallel()

	m := checked(t, found())
	frame := plain(m.Frame())

	if !strings.Contains(frame, "typescript 2339: Property 'is_archived'") {
		t.Errorf("the note is not under its line:\n%s", frame)
	}

	// A note on a line the change only carried is context rather than a
	// finding, so it stays out of the diff and waits in the trouble list.
	if strings.Contains(frame, "on a line this change only carried") {
		t.Errorf("a note on an unchanged line was drawn in the diff:\n%s", frame)
	}

	if !strings.Contains(frame, "2 problems") {
		t.Errorf("the title does not count the findings:\n%s", frame)
	}
}

// X is the list read the other way round: what is wrong anywhere, for a reader
// deciding what to open rather than reading the diff through.
func TestTroubleListsEverythingFound(t *testing.T) {
	t.Parallel()

	m := checked(t, found())
	press(m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	frame := plain(m.Frame())
	for _, want := range []string{
		"trouble", "3 problems",
		"typescript 2339", "house rules long-comment",
		"line 14 · house rules naming",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("the trouble list is missing %q:\n%s", want, frame)
		}
	}

	press(m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	if strings.Contains(plain(m.Frame()), "line 14 · house rules") {
		t.Error("a second X did not leave the list")
	}
}

// ]p walks the notes rather than the lines a long one wrapped onto, which is
// the same grammar every other object is reached by.
func TestProblemMotionWalksTheNotes(t *testing.T) {
	t.Parallel()

	m := checked(t, found())

	press(m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(m, tea.KeyPressMsg{Code: 'p', Text: "p"})

	if line := m.CursorText(); !strings.Contains(line, "typescript 2339") {
		t.Errorf("]p landed on %q, want the first note", line)
	}

	press(m, tea.KeyPressMsg{Code: 'n', Text: "n"})

	if line := m.CursorText(); !strings.Contains(line, "long-comment") {
		t.Errorf("n after ]p landed on %q, want the next note", line)
	}
}

// A pass that could not run has to say so, because a trouble list that is empty
// because nothing ran reads exactly like one that is empty because the change
// is clean.
func TestAFailedPassSaysSo(t *testing.T) {
	t.Parallel()

	m := checked(t, &prober{err: errNoServer})

	if frame := plain(m.Frame()); !strings.Contains(frame, "gopls is not installed") {
		t.Errorf("a pass that failed said nothing:\n%s", frame)
	}
}

func TestTroubleSaysWhenNothingCanCheck(t *testing.T) {
	t.Parallel()

	m := triaged(t)
	press(m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "nothing here can check") {
		t.Errorf("X on a review with no checker said:\n%s", frame)
	}
}

// K is the other question a diff cannot settle: what the names on a line
// actually are.
func TestHoverShowsWhatTheNamesAre(t *testing.T) {
	t.Parallel()

	m := checked(t, found())

	press(m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	press(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	press(m, tea.KeyPressMsg{Code: 'K', Text: "K"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "function split(r: io.Reader)") {
		t.Errorf("the hover answer is not on screen:\n%s", frame)
	}

	press(m, tea.KeyPressMsg{Code: 'j', Text: "j"})

	if strings.Contains(plain(m.Frame()), "function split(r: io.Reader)") {
		t.Error("the answer stayed up after a key")
	}
}

func TestFramesWithTrouble(t *testing.T) {
	t.Parallel()

	m := checked(t, found())
	golden.RequireEqual(t, []byte(plain(m.Frame())))
}

// A type error names the whole of an anonymous type, which against a generated
// API schema runs to hundreds of characters. Drawn in full it buries the diff
// under a message whose first line already said what is wrong.
func TestALongNoteIsCappedWhereItIsDrawn(t *testing.T) {
	t.Parallel()

	m := checked(t, &prober{notes: []diag.Note{{
		Path: parsed, Line: 15, Source: "typescript", Code: "2339",
		Message: "Property 'is_archived' does not exist on type '{ " +
			strings.Repeat("alpha: string; ", 60) + "}'.",
		Severity: diag.Error,
	}}})

	lines := strings.Split(plain(m.Frame()), "\n")

	var drawn []string

	for _, line := range lines {
		if strings.Contains(line, "alpha: string") {
			drawn = append(drawn, line)
		}
	}

	if len(drawn) == 0 || len(drawn) > 3 {
		t.Fatalf("the note drew %d rows:\n%s", len(drawn), strings.Join(drawn, "\n"))
	}

	if !strings.Contains(drawn[0], "typescript 2339: Property 'is_archived'") {
		t.Errorf("the first row lost the finding: %q", drawn[0])
	}

	if !strings.HasSuffix(strings.TrimRight(drawn[len(drawn)-1], " "), "…") {
		t.Errorf("a cut note does not say it was cut: %q", drawn[len(drawn)-1])
	}
}

// The hover overlay does not scroll, so a line of long names has to be counted
// rather than drawn: a frame taller than the terminal loses its own footer off
// the top of the screen.
func TestHoverFitsTheFrame(t *testing.T) {
	t.Parallel()

	syms := make([]diag.Symbol, 0, 12)
	for i := range 12 {
		syms = append(syms, diag.Symbol{
			Name: "name", Text: "(parameter) row: { " + strings.Repeat("alpha: string; ", 30) + "}",
		})
		syms[i].Name += string(rune('a' + i))
	}

	m := checked(t, &prober{syms: syms})

	press(m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	press(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	press(m, tea.KeyPressMsg{Code: 'K', Text: "K"})

	frame := plain(m.Frame())
	if got := len(strings.Split(frame, "\n")); got > 30 {
		t.Errorf("the hover answer drew %d rows into a 30-row frame:\n%s", got, frame)
	}

	if !strings.Contains(frame, "not shown") {
		t.Errorf("the names left out are not counted:\n%s", frame)
	}
}

// The footer message that announced a failed pass is gone by the time anyone
// presses X, so the empty list has to say the same thing on its own.
func TestTroubleSaysWhenThePassFailed(t *testing.T) {
	t.Parallel()

	m := checked(t, &prober{err: errNoServer})
	press(m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "nothing ran: gopls is not installed") {
		t.Errorf("the empty list reads as a clean change:\n%s", frame)
	}
}
