package tui_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// errRefused is a reader that cannot answer, which is the commit's reader
// asked about a path that is not the commit's.
var errRefused = errors.New("the commit cannot read an absolute path")

// prober stands in for a language server and the configured checks. The
// protocol itself is covered against a real server in internal/diag/lsp; what
// is worth driving here is what the screen does with the answers.
type prober struct {
	notes  []diag.Note
	syms   []diag.Symbol
	jump   diag.Jump
	err    error
	closed bool
	asked  []ask
}

// ask is one question put to Jumps.
type ask struct {
	line, col int
	kind      diag.JumpKind
}

func (p *prober) Notes(context.Context) ([]diag.Note, error) { return p.notes, p.err }

func (p *prober) Hover(context.Context, string, int) ([]diag.Symbol, error) {
	return p.syms, p.err
}

func (p *prober) Jumps(_ context.Context, _ string, line, col int, kind diag.JumpKind) (diag.Jump, error) {
	p.asked = append(p.asked, ask{line: line, col: col, kind: kind})

	return p.jump, p.err
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
func checked(t *testing.T, p tui.Prober, cs ...artifact.Comment) *tui.Model {
	t.Helper()

	m := checkable(t, p, cs...)
	m.Checked()

	return m
}

// checkable is the review with a checker attached but not yet answered, which
// is what the screen is for the seconds a real pass takes.
func checkable(t *testing.T, p tui.Prober, cs ...artifact.Comment) *tui.Model {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pr-42.toml")

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment, Comments: cs,
	}
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, (&counter{}).post,
		tui.WithProber(p))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

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
	press(t, m, tea.KeyPressMsg{Code: 'X', Text: "X"})

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

	press(t, m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	if strings.Contains(plain(m.Frame()), "line 14 · house rules") {
		t.Error("a second X did not leave the list")
	}
}

// ]p walks the notes rather than the lines a long one wrapped onto, which is
// the same grammar every other object is reached by.
func TestProblemMotionWalksTheNotes(t *testing.T) {
	t.Parallel()

	m := checked(t, found())

	press(t, m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})

	if line := m.CursorText(); !strings.Contains(line, "typescript 2339") {
		t.Errorf("]p landed on %q, want the first note", line)
	}

	press(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})

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
	press(t, m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "nothing here can check") {
		t.Errorf("X on a review with no checker said:\n%s", frame)
	}
}

// K is the other question a diff cannot settle: what the names on a line
// actually are.
func TestHoverShowsWhatTheNamesAre(t *testing.T) {
	t.Parallel()

	m := checked(t, found())

	press(t, m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	press(t, m, tea.KeyPressMsg{Code: 'K', Text: "K"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "function split(r: io.Reader)") {
		t.Errorf("the hover answer is not on screen:\n%s", frame)
	}

	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})

	if strings.Contains(plain(m.Frame()), "function split(r: io.Reader)") {
		t.Error("the answer stayed up after a key")
	}
}

// splitLine is the added line with four names on it, asked about at the byte
// column each one starts at.
const splitLine = "lines, err := split(r)"

// walkToSplit moves the cursor down to the row carrying splitLine.
func walkToSplit(t *testing.T, m *tui.Model) {
	t.Helper()

	for range 20 {
		if strings.Contains(cursorText(m), splitLine) {
			return
		}

		press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	t.Fatalf("the cursor never reached %q:\n%s", splitLine, plain(m.Frame()))
}

// cursorText is the frame line the cursor bar is on.
func cursorText(m *tui.Model) string {
	for _, line := range strings.Split(plain(m.Frame()), "\n") {
		if strings.HasPrefix(line, "▌") {
			return line
		}
	}

	return ""
}

// framed pins one frame of a scenario under its own golden name.
func framed(t *testing.T, name, frame string) {
	t.Helper()

	t.Run(name, func(t *testing.T) { golden.RequireEqual(t, []byte(frame)) })
}

func resize(m *tui.Model, width, height int) {
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
}

// gd and gr label every name on a line with a letter and ask about the one the
// letter picks: the label draws over the name, the letter reaches the server as
// that name's column, and the answer is a card under the line, at both ends of
// the sizes the screen runs at.
func TestGoLabelsTheNamesAndPeeksAtTheOneChosen(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		verb   rune
		kind   diag.JumpKind
		answer diag.Jump
		want   []string
	}{
		{
			"definition", 'd', diag.Definitions,
			diag.Jump{Name: "split", At: []diag.Site{{Path: "io/read.go", Line: 4}}},
			[]string{"⇢ split · io/read.go:4", "› 4  func split(r io.Reader)", "[enter] open · [j/k] · [esc] close"},
		},
		{
			"references", 'r', diag.References,
			diag.Jump{Name: "split", At: []diag.Site{{Path: "io/read.go", Line: 3}, {Path: "io/read.go", Line: 4}}},
			[]string{
				"⇢ split · 2 references", "io/read.go:3  // split reads r into fields.", "io/read.go:4  func split",
			},
		},
	} {
		for _, size := range [][2]int{{80, 24}, {200, 60}} {
			t.Run(fmt.Sprintf("%s/%dx%d", tt.name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				p := &prober{jump: tt.answer}
				m := peekable(t, p)
				resize(m, size[0], size[1])

				go2(t, m, ']', 'h')
				walkToSplit(t, m)
				press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
				press(t, m, tea.KeyPressMsg{Code: tt.verb, Text: string(tt.verb)})

				framed(t, "labels", plain(m.Frame()))

				if row := cursorText(m); !strings.Contains(row, "aines, srr := dplit(f)") {
					t.Fatalf("the names are not labeled a, s, d, f:\n%s", row)
				}

				drive(m, tea.KeyPressMsg{Code: 'd', Text: "d"})

				want := []ask{{line: 15, col: len("\tlines, err := "), kind: tt.kind}}
				if fmt.Sprint(p.asked) != fmt.Sprint(want) {
					t.Fatalf("asked %+v, want %+v", p.asked, want)
				}

				card := plain(m.Frame())
				framed(t, "card", card)

				for _, want := range tt.want {
					if !strings.Contains(card, want) {
						t.Errorf("the card does not draw %q:\n%s", want, card)
					}
				}

				fitsWidth(t, "the card", card, size[0])
			})
		}
	}
}

// A line with one name has nothing to choose between, so it asks at once; and
// esc gives up a pick without asking anyone.
func TestGoAsksAtOnceForOneNameAndEscCancelsAPick(t *testing.T) {
	t.Parallel()

	p := &prober{jump: diag.Jump{Name: "first", At: []diag.Site{{Path: "io/read.go", Line: 4}}}}
	m := peekable(t, p)

	go2(t, m, ']', 'h')
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	drive(m, tea.KeyPressMsg{Code: 'd', Text: "d"})

	if len(p.asked) != 1 || p.asked[0].col != len("\t") {
		t.Fatalf("the only name was not asked about at once: %+v", p.asked)
	}

	if frame := plain(m.Frame()); !strings.Contains(frame, "⇢ first · io/read.go:4") {
		t.Errorf("the card is not under the line:\n%s", frame)
	}

	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	walkToSplit(t, m)
	go2(t, m, 'g', 'd')
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(p.asked) != 1 || strings.Contains(cursorText(m), "aines") {
		t.Errorf("esc did not give up the pick: asked %+v\n%s", p.asked, cursorText(m))
	}
}

// A peek is a card in the walk: esc takes it down, and so does moving off it,
// and either leaves the cursor on the line that asked.
func TestPeekClosesOnEscAndOnMovingOff(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"esc", []tea.KeyPressMsg{{Code: tea.KeyEscape}}},
		{"up past the title", []tea.KeyPressMsg{
			{Code: 'k', Text: "k"}, {Code: 'k', Text: "k"}, {Code: 'k', Text: "k"}, {Code: 'k', Text: "k"},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := peekable(t, &prober{jump: diag.Jump{Name: "split", At: []diag.Site{{Path: "io/read.go", Line: 4}}}})

			go2(t, m, ']', 'h')
			walkToSplit(t, m)
			press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
			press(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
			drive(m, tea.KeyPressMsg{Code: 'd', Text: "d"})

			if !strings.Contains(plain(m.Frame()), "⇢ split") {
				t.Fatalf("no card to close:\n%s", plain(m.Frame()))
			}

			for _, k := range tt.keys {
				press(t, m, k)
			}

			frame := plain(m.Frame())
			if strings.Contains(frame, "⇢ split") || !strings.Contains(cursorText(m), splitLine) {
				t.Errorf("the card is still up or the cursor left its line:\n%s", frame)
			}
		})
	}
}

// References are rows the cursor walks, and a definition outside the checkout
// is read from the copy the server resolved, which is where most names live.
func TestPeekWalksReferencesAndReadsOutsideTheCheckout(t *testing.T) {
	t.Parallel()

	site := filepath.Join(t.TempDir(), "dep.go")
	if err := os.WriteFile(site, []byte("package dep\n\n// Thing is shared.\nfunc Thing() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &prober{jump: diag.Jump{Name: "Thing", At: []diag.Site{{Path: site, Line: 3}, {Path: site, Line: 4}}}}
	m := peekableWith(t, p, func(context.Context, string) ([]string, error) { return nil, errRefused })

	go2(t, m, ']', 'h')
	walkToSplit(t, m)
	press(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	press(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	drive(m, tea.KeyPressMsg{Code: 'd', Text: "d"})

	if row := cursorText(m); !strings.Contains(row, "dep.go:3  // Thing is shared.") {
		t.Fatalf("the cursor is not on the first reference:\n%s", plain(m.Frame()))
	}

	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})

	if row := cursorText(m); !strings.Contains(row, "dep.go:4  func Thing() {}") {
		t.Errorf("j did not walk to the second reference:\n%s", plain(m.Frame()))
	}
}

// K and the g chord refuse the same way when nothing can answer, and the
// refusal names which thing is missing: a checkout C could claim, or a server
// for the files under review.
func TestNoProberSaysTheCause(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		tree tui.Tree
		want string
	}{
		{"no checkout", tui.TreeNone, "a language server runs in a checkout"},
		{"a checkout", tui.TreeOnHead, "no language server is configured for these files"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "pr-42.toml")

			r := &artifact.Review{
				Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
				HeadSHA: "a1b2c3d", Event: artifact.EventComment,
			}
			if err := artifact.Save(path, r); err != nil {
				t.Fatal(err)
			}

			m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, (&counter{}).post,
				tui.WithTree(tt.tree))
			m.Init()
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

			press(t, m, tea.KeyPressMsg{Code: 'K', Text: "K"})

			if frame := plain(m.Frame()); !strings.Contains(frame, tt.want) {
				t.Errorf("K with nothing to ask said:\n%s", frame)
			}
		})
	}
}

// peekable is the review with a checker and a way to read the files a jump
// points into, which is what an excerpt is drawn from.
func peekable(t *testing.T, p tui.Prober) *tui.Model {
	t.Helper()

	return peekableWith(t, p, func(_ context.Context, _ string) ([]string, error) {
		return []string{
			"package io", "", "// split reads r into fields.",
			"func split(r io.Reader) ([]string, error) {",
			"\treturn fields(string(readAll(r))), nil", "}", "",
		}, nil
	})
}

func peekableWith(t *testing.T, p tui.Prober, read tui.Blobs) *tui.Model {
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
		tui.WithProber(p), tui.WithBlobs(read))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m
}

// drive presses a key and feeds what it set in motion back until nothing is
// left, which press() does not reach: a batch's children each answer with a
// message of their own.
func drive(m *tui.Model, k tea.KeyPressMsg) {
	const patience = 100 * time.Millisecond

	queue := []tea.Cmd{func() tea.Msg { _, cmd := m.Update(k); return ranMsg{cmd} }}
	for len(queue) > 0 {
		cmd := queue[0]
		queue = queue[1:]

		if cmd == nil {
			continue
		}

		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()

		var msg tea.Msg

		select {
		case msg = <-done:
		case <-time.After(patience):
			continue
		}

		switch msg := msg.(type) {
		case ranMsg:
			queue = append(queue, msg.cmd)
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

// ranMsg carries the command an Update returned so the loop above can run it.
type ranMsg struct{ cmd tea.Cmd }

// gg keeps the top g used to reach on its own.
func TestGGIsStillTheTop(t *testing.T) {
	t.Parallel()

	m := checked(t, found())

	press(t, m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	bottom := plain(m.Frame())

	go2(t, m, 'g', 'g')

	m2 := checked(t, found())
	if top := plain(m.Frame()); top != plain(m2.Frame()) {
		t.Errorf("gg did not come back to the first frame:\n%s\nbottom was:\n%s", top, bottom)
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

	press(t, m, tea.KeyPressMsg{Code: ']', Text: "]"})
	press(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	press(t, m, tea.KeyPressMsg{Code: 'K', Text: "K"})

	frame := plain(m.Frame())
	if got := len(strings.Split(frame, "\n")); got > 30 {
		t.Errorf("the hover answer drew %d rows into a 30-row frame:\n%s", got, frame)
	}

	if !strings.Contains(frame, "not shown") {
		t.Errorf("the names left out are not counted:\n%s", frame)
	}
}

// tab walks what wants a decision, and a note wants none: it is read where it
// lies or in the list, never stopped on.
func TestTabWalksPastANote(t *testing.T) {
	t.Parallel()

	m := checked(t, found(), comment("c1", parsed, artifact.SideRight, 16, "the finding"))

	press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})

	if got := m.CommentUnderCursor(); got != 0 {
		t.Errorf("tab landed on %q, want the comment", m.CursorText())
	}
}

// The pass answers seconds after the screen has a reader, and the rows it
// inserts shift every row number after them; the cursor must not slide with it.
func TestLateNotesKeepTheLineUnderTheCursor(t *testing.T) {
	t.Parallel()

	m := checkable(t, found())

	go2(t, m, ']', 'h')
	go2(t, m, ']', 'h')

	row, want := m.CursorRow(), m.CursorText()

	m.Checked()

	if m.CursorRow() == row {
		t.Fatal("the pass inserted no rows, which is not the case being pinned")
	}

	if got := m.CursorText(); got != want {
		t.Errorf("the cursor moved from %q to %q", want, got)
	}
}

// The footer message that announced a failed pass is gone by the time anyone
// presses X, so the empty list has to say the same thing on its own.
func TestTroubleSaysWhenThePassFailed(t *testing.T) {
	t.Parallel()

	m := checked(t, &prober{err: errNoServer})
	press(t, m, tea.KeyPressMsg{Code: 'X', Text: "X"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "nothing ran: gopls is not installed") {
		t.Errorf("the empty list reads as a clean change:\n%s", frame)
	}
}
