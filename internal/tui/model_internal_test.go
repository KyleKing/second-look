package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/seen"
	"github.com/kyleking/aragonite/tui/palette"
	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/acp"
	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/labels"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/threads"
)

// onePatch is one file with one hunk, which is as much diff as a rule about
// the screen around it needs.
const onePatch = `diff --git a/pkg/read.go b/pkg/read.go
index 1111111..2222222 100644
--- a/pkg/read.go
+++ b/pkg/read.go
@@ -1,3 +1,4 @@
 package pkg
-func Read() error { return nil }
+func Read(path string) error {
+	return open(path)
 }
`

// model is a review screen at 100 by 30 with nothing run behind its first
// frame, so a test reaches into it and asks one function at a time.
func model(t *testing.T, patch string, edit func(*artifact.Review), opts ...Option) *Model {
	t.Helper()

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}
	if edit != nil {
		edit(r)
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	submit := func(context.Context, *artifact.Review) (string, error) { return "", nil }

	m := New(t.Context(), r, diff.Parse([]byte(patch)), path, submit, opts...)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m
}

// rowsOf is the index of every row of kind holding comment, in order.
func rowsOf(m *Model, comment int) []int {
	var out []int

	for i := range m.screen.rows {
		if r := &m.screen.rows[i]; r.comment == comment && (r.kind == rowComment || r.kind == rowNote) {
			out = append(out, i)
		}
	}

	return out
}

// The review's body and note are kept apart by a blank row wherever either is
// drawn in full, and are not where neither is: a note opening where the body
// ended reads as more of the body.
func checkTheReviewsBodyAndNoteAreKeptApart(t *testing.T) {
	t.Helper()

	for _, tc := range []struct {
		name       string
		body, note string
		openNote   bool
		gap        bool
	}{
		{"a one-line body", "Looks good.", "", false, true},
		{"a one-line open note", "", "ran the suite", true, true},
		{"a folded note alone", "", "ran the suite", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := model(t, onePatch, func(r *artifact.Review) { r.Body, r.Note = tc.body, tc.note })
			if tc.openNote {
				m.folded.notes[reviewNote] = true
				m.rebuild()
			}

			body, note := rowsOf(m, reviewBody), rowsOf(m, reviewNote)
			if len(body) == 0 || len(note) == 0 {
				t.Fatalf("body rows %v, note rows %v", body, note)
			}

			// The separator is a row of the body's that holds the note's index.
			lastBody := body[len(body)-1]
			gap := lastBody+1 < len(m.screen.rows) && m.screen.rows[lastBody+1].kind == rowComment &&
				m.screen.rows[lastBody+1].comment == reviewNote && !m.screen.rows[lastBody+1].head

			if gap != tc.gap {
				t.Errorf("a gap after the body = %v, want %v (body %v, note %v)", gap, tc.gap, body, note)
			}
		})
	}
}

// stubProber answers nothing, which is enough for a key that refuses before it asks.
type stubProber struct{}

func (stubProber) Notes(context.Context) ([]diag.Note, error)                { return nil, nil }
func (stubProber) Hover(context.Context, string, int) ([]diag.Symbol, error) { return nil, nil }
func (stubProber) Jumps(context.Context, string, int, int, diag.JumpKind) (diag.Jump, error) {
	return diag.Jump{}, nil
}

func (stubProber) Calls(context.Context, string, int) (diag.Graph, error) { return diag.Graph{}, nil }
func (stubProber) Server(string) (string, bool)                           { return "", false }
func (stubProber) Close()                                                 {}

// say is the footer's text and whether it reads as a failure, which is the
// whole of what a refused key leaves behind.
func said(t *testing.T, m *Model, text string, failed bool) {
	t.Helper()

	if m.status != text || m.failed != failed {
		t.Errorf("the footer says %q (failed %v), want %q (failed %v)", m.status, m.failed, text, failed)
	}
}

// What a key says when it has nothing to act on, and whether it says it as a
// failure: a refusal the reader caused is not one, and a question that broke is.
func checkAKeyThatCannotActSaysWhy(t *testing.T) {
	t.Helper()

	t.Run("g off a line of code", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil, WithProber(stubProber{}))
		m.cursor = 0
		m.status, m.failed = "before", true

		if cmd := m.calls(); cmd != nil {
			t.Error("g off a line of code asked anyway")
		}

		said(t, m, "g's calls asks about a line of the change", false)
	})

	t.Run("L off a lockfile", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil, WithAdvisor(stubAdvisor{}))
		m.cursor = 0
		m.askAdvisoriesNow()

		said(t, m, "advisories are asked about a lockfile, and this is not one", true)
	})

	t.Run("an advisory question that broke", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil)
		m.applyAdvisories(advisedMsg{path: "go.sum", err: errDown})

		said(t, m, "advisories: "+errDown.Error(), true)

		if got := m.advised["go.sum"]; got.failed != errDown.Error() {
			t.Errorf("the lockfile's row holds %+v", got)
		}

		m.applyAdvisories(advisedMsg{path: "go.sum"})
		said(t, m, "no advisories", false)
	})
}

type stubAdvisor struct{}

func (stubAdvisor) Ask(context.Context, []advisory.Package) (map[advisory.Package][]advisory.Note, error) {
	return map[advisory.Package][]advisory.Note{}, nil
}

// errDown is whatever the far side of a seam answered with when it failed.
var errDown = errors.New("the far side is down")

// A offers the three asker questions only where an asker is configured; a
// question about a line is kept for an agent either way.
func checkAskObjectsFollowTheAsker(t *testing.T) {
	t.Helper()

	without := model(t, onePatch, nil)
	if got := without.askObjects(); len(got) != 1 || got[0][0] != "l" {
		t.Errorf("without an asker A offers %v", got)
	}

	with := model(t, onePatch, nil, WithAsker(func(context.Context, string) (string, error) { return "", nil }))
	if got := with.askObjects(); len(got) != len(allAskObjects()) {
		t.Errorf("with an asker A offers %v", got)
	}
}

// The agent's clock re-arms only while a turn is in flight on a live pane, and
// a queued prompt goes on the wire once, however often the queue is pumped.
func checkTheAgentPaneTicksOnlyWhileATurnIsOut(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	m.agent, m.agentOpen, m.agentSending = &acp.Session{}, true, true

	if m.agentTicked() == nil {
		t.Error("a turn in flight let the clock lapse")
	}

	m.agentDead = errDown
	if m.agentTicked() != nil {
		t.Error("a dead pane kept its clock running")
	}

	m.agentDead, m.agentSending = nil, false
	if m.agentTicked() != nil {
		t.Error("an idle pane kept its clock running")
	}

	m.agentQueue = []string{"first", "second"}
	if m.pumpAgent() == nil || !m.agentSending || len(m.agentQueue) != 1 {
		t.Fatalf("the first pump left sending=%v and %v queued", m.agentSending, m.agentQueue)
	}

	if m.pumpAgent() != nil || len(m.agentQueue) != 1 || m.agentQueue[0] != "second" {
		t.Errorf("a second pump started a parallel turn: %v queued", m.agentQueue)
	}
}

// A card's notes and turns read one step under its prose, and its head at the
// severity a card of its own would carry.
func checkACardsInnerRowsReadInTheirOwnFace(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	for _, kind := range []rowKind{rowNote, rowTurn} {
		got := m.bodyFace(row{kind: kind})
		if !got.GetItalic() || got.GetForeground() != m.styles.cardNote.GetForeground() {
			t.Errorf("row kind %d reads in %v, want the card's note face", kind, got)
		}
	}

	if got := m.bodyFace(row{kind: rowComment}); got.GetItalic() {
		t.Error("a comment's prose reads in the note face")
	}
}

// The foot of a card describes the row the cursor stands on inside it,
// including the very first row of the screen, and its own keys anywhere else.
func checkAFootDescribesTheRowUnderTheCursor(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	inside := row{kind: rowPeek, role: roleTop, text: "inside"}
	foot := row{kind: rowPeek, role: roleFoot, text: "foot"}
	m.screen.rows = []row{inside, {kind: rowCode, text: "outside"}, foot}

	for _, tc := range []struct {
		cursor int
		want   string
	}{{0, "inside"}, {1, "foot"}, {-1, "foot"}, {3, "foot"}} {
		m.cursor = tc.cursor
		if got := m.keysRow(foot).text; got != tc.want {
			t.Errorf("at row %d the foot describes %q, want %q", tc.cursor, got, tc.want)
		}
	}
}

// The call sites under a graph are drawn for a function called once as well as
// for one called often, and not for one never called.
func checkCallSitesAreDrawnForASingleCall(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	once := newCallGraph(
		diag.Graph{Callers: []diag.CallNode{{Name: "Load", Sites: []diag.Site{{Path: "pkg/read.go", Line: 2}}}}},
	)
	if rows := m.callSiteRows(once); len(rows) == 0 || rows[0].text != "call sites · pkg/read.go" {
		t.Errorf("one call site drew %+v", rows)
	}

	never := newCallGraph(diag.Graph{Callers: []diag.CallNode{{Name: "Load"}}})
	if rows := m.callSiteRows(never); rows != nil {
		t.Errorf("no call site drew %+v", rows)
	}
}

// T says what it would do: open an agent where one is configured, hand back
// whatever is marked todo, and draft otherwise. Handing back with nowhere to
// write the set is a failure the reader is told about.
func checkTNamesWhatItWouldDo(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	if got := m.dispatchWord(); got != draftWord {
		t.Errorf("with nothing todo T offers %q", got)
	}

	m.review.Comments = []artifact.Comment{
		{ID: "c1", Path: "pkg/read.go", Line: 2, Body: "x", Status: artifact.StatusTodo},
	}
	if got := m.dispatchWord(); got != "hand back" {
		t.Errorf("with one todo T offers %q", got)
	}

	if cmd := m.dispatch(); cmd != nil {
		t.Error("a set with nowhere to go was dispatched")
	}

	said(t, m, errNoStore.Error(), true)

	m.agentStart = func(context.Context, string) (*acp.Session, error) { return nil, errDown }
	if got := m.dispatchWord(); got != "agent" {
		t.Errorf("with an agent configured T offers %q", got)
	}
}

// editing opens an editor over row 0's comment, as e would.
func editing(t *testing.T, m *Model, store string) {
	t.Helper()

	m.store = store
	m.editing = &editor{area: newArea(), key: "c1", start: "as staged", msg: editedMsg{index: 0, replyTo: -1}}
	m.editing.area.SetWidth(20)
}

// The editor's own keys: ctrl+r drops a restored buffer from disk as well as
// from the field, ctrl+t with nothing to swap in is not a failure, a completion
// refits the box to what it now holds, and the cursor is put on the row the
// editor replaces however far down it was.
func checkTheEditorsOwnKeys(t *testing.T) {
	t.Helper()

	t.Run("ctrl+r", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil)
		store := t.TempDir()
		editing(t, m, store)

		if err := artifact.SaveDraft(store, "c1", "left unfinished"); err != nil {
			t.Fatal(err)
		}

		m.editing.restored = true
		m.editing.area.SetValue("left unfinished")
		m.putBack()

		if _, _, ok := artifact.LoadDraft(store, "c1"); ok {
			t.Error("the restored buffer is still on disk")
		}

		if got := m.editing.area.Value(); got != "as staged" || m.editing.restored {
			t.Errorf("the field reads %q, restored %v", got, m.editing.restored)
		}

		said(t, m, "dropped the unfinished edit", false)
	})

	t.Run("ctrl+t", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil)
		editing(t, m, "")
		m.failed = true
		m.swapTheirs()
		said(t, m, "nothing arrived under this one", false)

		m.editing.theirs = "the agent's"
		m.editing.area.SetValue("mine")
		m.swapTheirs()

		if m.editing.area.Value() != "the agent's" || m.editing.theirs != "mine" {
			t.Errorf("the swap left %q and held %q", m.editing.area.Value(), m.editing.theirs)
		}
	})

	t.Run("completion", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil)
		editing(t, m, "")
		m.editing.area.SetHeight(editorMax)
		m.replaceWord(0, "a few words that wrap")

		if got := m.editing.area.Height(); got != 2 {
			t.Errorf("the box is %d rows over two rows of text", got)
		}
	})

	t.Run("to the head", func(t *testing.T) {
		t.Parallel()

		m := model(t, onePatch, nil)
		editing(t, m, "")
		m.screen.rows = []row{
			{kind: rowComment, comment: 0, head: true},
			{kind: rowComment, comment: 0},
			{kind: rowComment, comment: 0},
		}
		m.cursor = 2
		m.toHead()

		if m.cursor != 0 {
			t.Errorf("the cursor is on row %d, want the head on row 0", m.cursor)
		}
	})
}

// The cosmetic level hides a hunk that is whitespace only or that the parser
// called cosmetic; the whitespace level hides only the first.
func checkFoldLevelsHideWhatTheyName(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	m.cosmetic = map[hunkAt]bool{{"pkg/read.go", 0}: true}

	for _, tc := range []struct {
		level foldLevel
		want  bool
	}{{foldCosmetic, true}, {foldWhitespace, false}, {foldNone, false}} {
		m.fold = tc.level

		h := m.level()
		if got := h.skip != nil && h.skip("pkg/read.go", 0); got != tc.want {
			t.Errorf("level %v hides the cosmetic hunk = %v, want %v", tc.level, got, tc.want)
		}
	}
}

// The header says where the code under review is when it is not the head.
func checkTheHeaderSaysWhereTheTreeIs(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	for tree, want := range map[Tree]string{TreeOnHead: "", TreeElsewhere: "off head", TreeNone: "no clone"} {
		m.tree = tree

		found := ""
		for _, f := range m.facts(nil) {
			if strings.Contains(f.text, "off head") || strings.Contains(f.text, "no clone") {
				found = xansi.Strip(f.text)
			}
		}

		if found != want {
			t.Errorf("tree %v says %q, want %q", tree, found, want)
		}
	}
}

// The label picker's note is cut to the box, not to the frame.
func checkTheLabelPickersNoteFitsTheBox(t *testing.T) {
	t.Helper()

	p, _ := newLabelPicker(labels.Target{Repo: "o/r", Number: 1}, nil)
	p.loading = false
	p.note = strings.Repeat("abcdefghij", 10)

	body := make([]string, 20)
	for i := range body {
		body[i] = strings.Repeat(" ", 60)
	}

	got := xansi.Strip(strings.Join(p.box(loadStyles(), body, 60), "\n"))
	if want := p.note[:60-boxMargin-1] + "…"; !strings.Contains(got, want) {
		t.Errorf("the note is not cut to %d cells:\n%s", 60-boxMargin, got)
	}
}

// longList is a queue tab of n rows.
func longList(n int) *List {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{
			Key:   strconv.Itoa(i),
			Cells: map[string]Cell{"title": {Spans: []Span{{Text: fmt.Sprint("row ", i)}}}},
		}
	}

	l := NewTabs([]Tab{{
		Name:     "queue",
		Sections: func() []Section { return []Section{{Name: "all", Rows: rows}} },
		Columns:  func() []table.Column { return []table.Column{{Key: "title", Min: 10, Weight: 1}} },
	}}, 0)
	l.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	return l
}

// Moving up keeps scrollOff lines between the cursor and the top of the frame.
func checkTheListScrollsAheadOfTheCursor(t *testing.T) {
	t.Helper()

	l := longList(60)
	l.offset, l.cursor = 40, 40
	l.scroll()

	if l.offset != 40-scrollOff {
		t.Errorf("moving up to row 40 scrolled to %d, want %d", l.offset, 40-scrollOff)
	}

	l.offset, l.cursor = 0, 1
	l.scroll()

	if l.offset != 0 {
		t.Errorf("the top of the list scrolled to %d", l.offset)
	}
}

// A label listing reaches the picker it was asked for.
func checkTheListHandsALabelListingToItsPicker(t *testing.T) {
	t.Helper()

	l := longList(3)
	at := labels.Target{Repo: "o/r", Number: 1}
	l.labeler, _ = newLabelPicker(at, nil)

	l.labels(labelsReadMsg{at: at, all: []labels.Label{{Name: "bug"}}})

	if l.labeler.loading || len(l.labeler.all) != 1 {
		t.Errorf("the picker holds %+v, loading %v", l.labeler.all, l.labeler.loading)
	}
}

// The preview pane is titled with the row's subject, its prefix, and its ref,
// and says how much of the detail the pane's interior holds.
func checkThePreviewPaneSaysHowMuchItHolds(t *testing.T) {
	t.Helper()

	l := longList(3)
	r := l.current()
	r.About = &About{Subject: "wrap the error", Prefix: "fix", Ref: "o/r#7"}

	for i := range 12 {
		r.Detail = append(r.Detail, fmt.Sprint("detail ", i))
	}

	out := l.previewPane(48, 6)
	if len(out) != 6 {
		t.Fatalf("the pane is %d lines, want 6", len(out))
	}

	want := []string{"fix  wrap the error  o/r#7  5/12", "detail 0", "detail 1", "detail 2", "detail 3", "detail 4"}
	for i, line := range out {
		if got := strings.TrimRight(xansi.Strip(line), " "); got != want[i] {
			t.Errorf("line %d reads %q, want %q", i, got, want[i])
		}
	}
}

// While a rebuild is reading the model off the loop, a key that changes the
// review waits for it and a key that only moves does not.
func checkAKeyWaitsOutARebuildUnlessItOnlyMoves(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	if m.deferBuild(tea.KeyPressMsg{Code: 'r', Text: "r"}) {
		t.Error("a key was held with no rebuild running")
	}

	m.building = true
	if !m.deferBuild(tea.KeyPressMsg{Code: 'r', Text: "r"}) || len(m.deferred) != 1 || !m.buildQueued {
		t.Errorf("an action key during a rebuild was not held: %v", m.deferred)
	}

	if m.deferBuild(tea.KeyPressMsg{Code: 'j', Text: "j"}) || len(m.deferred) != 1 {
		t.Errorf("a motion during a rebuild was held: %v", m.deferred)
	}

	if !m.moved(tea.KeyPressMsg{Code: 'j', Text: "j"}) || m.moved(tea.KeyPressMsg{Code: 'r', Text: "r"}) {
		t.Error("moved does not tell a motion from an action")
	}
}

// Folding everything puts every conversation away, and opening everything
// brings them back.
func checkFoldAllReachesTheConversations(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	m.threads = make([]threads.Thread, 2)

	m.foldAll(false)
	if !m.folded.threads[0] || !m.folded.threads[1] {
		t.Errorf("zM left conversations open: %v", m.folded.threads)
	}

	m.foldAll(true)
	if m.folded.threads[0] || m.folded.threads[1] {
		t.Errorf("zR left conversations folded: %v", m.folded.threads)
	}
}

// The legend pages, and the about page's wheel stops where
// its last line reaches the bottom.
func checkOverlaysScrollByTheirOwnMeasure(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	m.openPalette(palette.Browse)
	first, _ := m.pal.Selected()
	m.pal.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	down, _ := m.pal.Selected()
	m.pal.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	back, _ := m.pal.Selected()

	if down.Action == first.Action || back.Action != first.Action {
		t.Errorf("paging the legend went %q, %q, %q; want away and back", first.Action, down.Action, back.Action)
	}

	m.pal.Close()
	m.aboutOpen = true
	m.about = threads.About{
		Title: "fix: wrap the error", Author: "alice", Added: 3, Removed: 1, Labels: []string{"bug"},
		Body: strings.Repeat("a paragraph of the description\n\n", 3*m.viewHeight()),
	}

	if got := xansi.Strip(m.aboutText(m.width)[2]); got != "   alice · +3 -1 · bug" {
		t.Errorf("the about page's facts read %q", got)
	}

	for range 4 * m.viewHeight() {
		m.wheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}

	if last := len(m.aboutText(m.width)) - m.viewHeight(); last <= 0 || m.aboutAt != last {
		t.Errorf("the wheel stopped the about page at %d, want %d", m.aboutAt, last)
	}
}

// A failure is said as one: a read mark that could not be written, and a
// merge GitHub refused, which also leaves the screen through Failure.
func checkFailuresSayTheirReason(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	m.read, m.seenAt = seen.New(), filepath.Join(blocked, "read.json")
	m.saveRead("marked read")

	if !m.failed || !strings.HasPrefix(m.status, "creating ") {
		t.Errorf("a read mark that could not be saved says %q (failed %v)", m.status, m.failed)
	}

	m.applyMerge(mergedMsg{err: errDown})
	said(t, m, errDown.Error(), true)

	if m.Failure() == nil || m.merged {
		t.Errorf("a refused merge left failure %v, merged %v", m.Failure(), m.merged)
	}

	m.applyMerge(mergedMsg{summary: "merged #42"})
	said(t, m, "merged #42, press q to leave", false)
}

// Landing on a block reveals all of it where it fits and keeps the line above
// it in view where it only just does not.
func checkRevealPlacesABlockByItsHeight(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	h := m.viewHeight()

	for _, tc := range []struct {
		name string
		tall int
		want int
	}{
		{"a block shorter than the frame is centered", h - 4, 20 - 2},
		{"a block exactly as tall as the frame keeps its margin", h, 20 - jumpMargin},
	} {
		rows := make([]row, 0, 20+tc.tall+60)
		for range 20 {
			rows = append(rows, row{kind: rowCode})
		}

		for range tc.tall {
			rows = append(rows, row{kind: rowComment, comment: 0})
		}

		for range 60 {
			rows = append(rows, row{kind: rowCode})
		}

		m.screen.rows, m.cursor = rows, 20
		m.reveal()

		if m.offset != tc.want {
			t.Errorf("%s: offset %d, want %d", tc.name, m.offset, tc.want)
		}
	}
}

// A diff of the async threshold's size is built off the loop.
func checkAHugeDiffIsBuiltOffTheLoop(t *testing.T) {
	t.Helper()

	var b strings.Builder

	b.WriteString("diff --git a/big.txt b/big.txt\n--- /dev/null\n+++ b/big.txt\n")
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", asyncRows)

	for i := range asyncRows {
		fmt.Fprintf(&b, "+line %d\n", i)
	}

	d := diff.Parse([]byte(b.String()))
	if diffRows(d) != asyncRows {
		t.Fatalf("the diff carries %d rows, want %d", diffRows(d), asyncRows)
	}

	if m := New(t.Context(), &artifact.Review{}, d, "", nil); !m.hugeDiff {
		t.Error("a diff at the threshold is built on the loop")
	}

	small := diff.Parse([]byte(onePatch))
	if m := New(t.Context(), &artifact.Review{}, small, "", nil); m.hugeDiff {
		t.Error("a small diff is built off the loop")
	}
}

// A folded file's heading is marked folded, which is what draws its lines away.
func checkAShutHeadIsFolded(t *testing.T) {
	t.Helper()

	head := shutHead(row{kind: rowFile, path: "go.sum"}, []string{"lockfile"}, &artifact.Review{}, nil)
	if !head.folded || head.path != "go.sum" {
		t.Errorf("the heading is %+v", head)
	}
}

// The agent's markdown keeps a quote's words whole and drops only its marker.
func checkAgentMarkdownDrawsEachMark(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	text := "# Title\n> quoted words\nplain `code` here\n---\n```\nfenced\n```"

	got := m.markdownLines(text, m.styles.body, 20)
	for i := range got {
		got[i] = xansi.Strip(got[i])
	}

	want := []string{"Title", "quoted words", "plain code here", strings.Repeat("─", 20), "fenced"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("drew %q, want %q", got, want)
	}
}

// Side by side, removals pair with the additions that replaced them in order,
// and whichever side runs longer keeps rows of its own with the other half blank.
func checkASplitBlockPairsInOrder(t *testing.T) {
	t.Helper()

	// lines reads "-r1,+a1, c" as a removal, an addition, and a context line.
	lines := func(spec string) []diff.Line {
		words := strings.Split(spec, ",")
		out := make([]diff.Line, 0, len(words))

		for _, word := range words {
			out = append(out, diff.Line{Kind: word[0], Text: word[1:]})
		}

		return out
	}
	show := func(rows []row) string {
		var b strings.Builder
		for i := range rows {
			r := &rows[i]
			fmt.Fprintf(&b, "%s|%s ", r.line.Text, r.mate.Text)
		}

		return b.String()
	}

	for _, tc := range []struct {
		name  string
		lines []diff.Line
		want  string
	}{
		{
			"more removed than added",
			lines("-r1,-r2,+a1, c"),
			"a1|r1 r2| c| ",
		},
		{
			"more added than removed",
			lines("-r1,+a1,+a2, c"),
			"a1|r1 a2| c| ",
		},
		{
			"a removal after an addition opens a new block",
			lines("+a1,-r1,+a2, c"),
			"a1| a2|r1 c| ",
		},
	} {
		var out []row

		p := pairer{split: true, out: &out, hang: func(diff.Line) []row { return nil }}
		for _, l := range tc.lines {
			p.take(l)
		}

		if got := show(out); got != tc.want {
			t.Errorf("%s: rows %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A definition card names how many other places define the name.
func checkADefinitionCardCountsTheRest(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	for _, tc := range []struct {
		sites int
		aside string
	}{{1, ""}, {2, "+1 more"}, {4, "+3 more"}} {
		sites := make([]diag.Site, tc.sites)
		for i := range sites {
			sites[i] = diag.Site{Path: "pkg/read.go", Line: i + 1}
		}

		m.peeked = &peekCard{kind: diag.Definitions, name: "Read", sites: sites}

		if got := m.peekRows()[0]; got.aside != tc.aside {
			t.Errorf("%d definitions: the heading's aside is %q, want %q", tc.sites, got.aside, tc.aside)
		}
	}
}

// A plan heading is drawn only where it says something the paths under it do not.
func checkAPlanHeadingIsNamedOnlyWhereItSaysSomething(t *testing.T) {
	t.Helper()

	var l layout

	cases := []struct {
		name   string
		g      planGroup
		groups int
		want   bool
	}{
		{"one plain directory", planGroup{mode: plan.ModeRead}, 1, false},
		{"one of several", planGroup{mode: plan.ModeRead}, 2, true},
		{"read another way", planGroup{mode: plan.ModeSkim}, 1, true},
		{"with a reason", planGroup{mode: plan.ModeRead, why: "the schema"}, 1, true},
		{"a stop", planGroup{mode: plan.ModeRead, stop: 1}, 1, true},
	}

	for i := range cases {
		tc := &cases[i]

		if got := l.named(tc.g, tc.groups); got != tc.want {
			t.Errorf("%s: named = %v, want %v", tc.name, got, tc.want)
		}
	}

	l.plan = []plan.Chapter{{Name: "Load", Symbol: true}}
	if !l.named(planGroup{mode: plan.ModeRead, chapter: 1}, 1) {
		t.Error("a symbol's chapter is not named")
	}
}

// The other end of a move is the line on the far side of it, never a context
// line that happens to carry the same number.
func checkTheOtherEndOfAMoveIsTheMovedLine(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	added := diff.Line{Kind: diff.KindAdd, New: 10, Text: "x"}
	m.moves = map[diff.LineRef]diff.Move{refOf("a.go", added): {Path: "a.go", Line: 3}}
	m.screen.rows = []row{
		{kind: rowFile, path: "a.go"},
		{kind: rowCode, path: "a.go", line: added},
		{kind: rowCode, path: "a.go", line: diff.Line{Kind: diff.KindContext, Old: 3, New: 3}},
		{kind: rowCode, path: "a.go", line: diff.Line{Kind: diff.KindAdd, New: 3}},
		{kind: rowCode, path: "a.go", line: diff.Line{Kind: diff.KindRemove, Old: 3, Text: "x"}},
	}
	m.cursor = 0
	m.otherEnd()

	if m.cursor != 4 {
		t.Errorf("the other end landed on row %d, want the removed line on row 4", m.cursor)
	}
}

// Comparing against an earlier round says what it hid, which is not a failure.
func checkComparingSaysWhatItHid(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	m.failed = true
	m.roundDiffs = map[string]*diff.Diff{"0123456789": diff.Parse([]byte(onePatch))}
	m.compareTo("0123456789", false)

	said(t, m, "showing what changed since 0123456; 1 hunk unchanged since then, hidden", false)
}

// Enter leaves the search prompt, and an empty pattern clears the footer
// rather than reading as a failure.
func checkSearchLeavesItsPrompt(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	m.begin()

	if !m.search.input.Focused() {
		t.Fatal("/ did not focus the prompt")
	}

	m.status, m.failed = "earlier", true
	m.typing(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.searching || m.search.input.Focused() {
		t.Errorf("enter left searching=%v, focused=%v", m.searching, m.search.input.Focused())
	}

	said(t, m, "", false)
}

// A shape's member is exact by its own file's answer, not its neighbor's.
func checkAShapeMemberIsExactByItsOwnFile(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	m.plan.Chapters = []plan.Chapter{{Name: "plain"}, {Name: "shape", Pattern: &plan.Pattern{Members: []plan.Member{
		{Path: "b.go", Beyond: []string{"+more"}},
		{Path: "a.go"},
	}}}}

	for name, want := range map[string]bool{"a.go": true, "b.go": false, "c.go": false} {
		if got := m.exactMember(name); got != want {
			t.Errorf("exactMember(%q) = %v, want %v", name, got, want)
		}
	}
}

// A hint is drawn where it fits exactly and dropped whole where it does not.
func checkAHintIsDrawnWholeOrNotAtAll(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	r := row{spans: []chunk{hintSpan("z", "fold")}}
	fits := textWidth("lead ") + textWidth(hintSpan("z", "fold").plain())

	if got := xansi.Strip(m.spanLine(r, "lead ", m.styles.body, fits, false)); got != "lead [z] fold" {
		t.Errorf("a hint with exactly its room drew %q", got)
	}

	if got := xansi.Strip(m.spanLine(r, "lead ", m.styles.body, fits-1, false)); got != "lead " {
		t.Errorf("a hint a cell short drew %q", got)
	}
}

// dirsPatch is n one-line files, one per directory, which the plan reads as
// n chapters.
func dirsPatch(n int) string {
	var b strings.Builder

	for i := range n {
		fmt.Fprintf(&b, "diff --git a/ch%02d/f.go b/ch%02d/f.go\n--- a/ch%02d/f.go\n+++ b/ch%02d/f.go\n"+
			"@@ -1 +1 @@\n-old\n+new\n", i, i, i, i)
	}

	return b.String()
}

// The tour's strip lights the group under the cursor and only that one, the
// rail is drawn from the tour's own width, and the next group is named with
// its stop's question.
func checkTheTourFollowsTheCursor(t *testing.T) {
	t.Helper()

	m := model(t, dirsPatch(5), nil)
	if !m.touring() || m.tourAt() != 0 {
		t.Fatalf("five chapters are not toured: touring %v at %d", m.touring(), m.tourAt())
	}

	lit, _, _ := strings.Cut(m.styles.selected.Bold(true).Render("x"), "x")
	if got := strings.Count(m.tourStrip(80), lit+"─"); got != 1 {
		t.Errorf("%d groups are lit on the strip, want the one under the cursor", got)
	}

	m.width = tourWidth
	if !m.tourWide() {
		t.Error("the rail is not drawn at exactly the tour's width")
	}

	m.width = tourWidth - 1
	if m.tourWide() {
		t.Error("the rail is drawn a column short of the tour's width")
	}

	if got := m.nextGroup(); got != "ch01" {
		t.Errorf("the next group is %q", got)
	}

	m.plan.Stops = []plan.Stop{{What: "first question"}, {What: "second question"}}
	m.screen.groups[1].stop = 2

	if got := m.nextGroup(); got != "ch01 · second question" {
		t.Errorf("the next stop is %q", got)
	}
}

// Below the tour's width the outline splits the frame: its list takes half and
// the detail the rest, whatever the number of chapters.
func checkTheNarrowOutlineSplitsTheFrame(t *testing.T) {
	t.Helper()

	m := model(t, dirsPatch(40), nil)
	h := m.viewHeight()

	out := m.outlineLines()
	if len(out) != h {
		t.Fatalf("the outline is %d lines over a %d-line frame", len(out), h)
	}

	if rule := xansi.Strip(out[h/2]); !strings.HasPrefix(rule, "───") {
		t.Errorf("line %d is %q, want the rule between list and detail", h/2, rule)
	}
}

// Where only the window right around the cursor fits with names, the strip
// draws that window named rather than dropping every name.
func checkTheStripKeepsTheNameUnderTheCursorWhileItFits(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)

	cs := make([]cluster, 5)
	for i := range cs {
		cs[i] = cluster{label: fmt.Sprintf("chapter-%d", i), segs: []seg{{weight: 1}}}
	}

	room := 0

	for r := 1; r < 200; r++ {
		_, tight := stripWidths(windowed(cs, 2, 0), true, r)
		_, wider := stripWidths(windowed(cs, 2, 1), true, r)

		if tight && !wider {
			room = r

			break
		}
	}

	if room == 0 {
		t.Fatal("no width fits the cursor's window alone")
	}

	got := xansi.Strip(m.fitClusters(cs, 2, room))
	if !strings.Contains(got, "chapter-2") || strings.Contains(got, "chapter-1") || strings.Count(got, "+2") != 2 {
		t.Errorf("at %d columns the strip reads %q, want chapter-2 named between two +2 groups", room, got)
	}
}

// A note under its line wraps to what the line's own gutter leaves.
func checkATroubleNoteWrapsInsideTheGutter(t *testing.T) {
	t.Helper()

	n := diag.Note{Source: "stub", Message: strings.Repeat("a long message about a type ", 4)}
	const numWidth, width = 4, 60

	room := width - numWidth - indent - troubleRail
	rows := troubleRows([]diag.Note{n}, "a.go", numWidth, width)

	if len(rows) < 2 || !rows[0].head || rows[1].head {
		t.Fatalf("the note drew %+v", rows)
	}

	for i := range rows {
		r := &rows[i]
		if w := textWidth(r.text); w > room || (i < len(rows)-1 && w < room-12) {
			t.Errorf("%q is %d wide, want it to fill up to %d", r.text, w, room)
		}
	}
}

// The screen takes the alternate screen and dims the keys it cannot act on.
func checkTheViewAndItsInertKeys(t *testing.T) {
	t.Helper()

	m := model(t, onePatch, nil)
	if !m.View().AltScreen {
		t.Error("the review is not drawn on the alternate screen")
	}

	if !m.inert("T") {
		t.Error("T is live with nothing to dispatch to")
	}

	m.dispatcher = func(context.Context, string, string) (string, error) { return "", nil }
	if m.inert("T") {
		t.Error("T is dim with a dispatcher configured")
	}

	if got := m.signCell('+'); xansi.Strip(got) != "+" {
		t.Errorf("an added line's sign is %q", got)
	}

	if got := m.signCell('?'); got != " " {
		t.Errorf("a sign nothing styles is %q", got)
	}
}

// Every file in the trouble list after the first is set off by a blank row,
// whatever the file before it held.
func checkTroubleFilesAreSetApart(t *testing.T) {
	t.Helper()

	d := diff.Parse([]byte(onePatch))
	note := []diag.Note{{Path: "b.go", Line: 40, Message: "wrong"}}

	for _, tc := range []struct {
		name string
		t    diag.Placed
	}{
		{"two files with notes", diag.Placed{Elsewhere: map[string][]diag.Note{
			"a.go": {{Path: "a.go", Line: 9, Message: "unused"}}, "b.go": note,
		}}},
		{"a file whose notes all went", diag.Placed{Elsewhere: map[string][]diag.Note{"a.go": {}, "b.go": note}}},
	} {
		s := buildTrouble(d, tc.t, false, nil, layout{width: 100})

		files := 0

		for i := range s.rows {
			r := &s.rows[i]
			if r.kind != rowFile {
				continue
			}

			files++

			if gap := i > 0 && s.rows[i-1].kind == rowBlank; gap != (files > 1) {
				t.Errorf("%s: file %d (%s) has a blank row above it = %v", tc.name, files, r.path, gap)
			}
		}

		if files != 2 {
			t.Errorf("%s: %d files listed, want 2", tc.name, files)
		}
	}
}

// TestModelRules runs each rule of the review screen that a frame would only show at one exact
// width or one exact state, against a model reached into directly.
func TestModelRules(t *testing.T) {
	t.Parallel()

	for name, check := range map[string]func(*testing.T){
		"TheReviewsBodyAndNoteAreKeptApart":             checkTheReviewsBodyAndNoteAreKeptApart,
		"AKeyThatCannotActSaysWhy":                      checkAKeyThatCannotActSaysWhy,
		"AskObjectsFollowTheAsker":                      checkAskObjectsFollowTheAsker,
		"TheAgentPaneTicksOnlyWhileATurnIsOut":          checkTheAgentPaneTicksOnlyWhileATurnIsOut,
		"ACardsInnerRowsReadInTheirOwnFace":             checkACardsInnerRowsReadInTheirOwnFace,
		"AFootDescribesTheRowUnderTheCursor":            checkAFootDescribesTheRowUnderTheCursor,
		"CallSitesAreDrawnForASingleCall":               checkCallSitesAreDrawnForASingleCall,
		"TNamesWhatItWouldDo":                           checkTNamesWhatItWouldDo,
		"TheEditorsOwnKeys":                             checkTheEditorsOwnKeys,
		"FoldLevelsHideWhatTheyName":                    checkFoldLevelsHideWhatTheyName,
		"TheHeaderSaysWhereTheTreeIs":                   checkTheHeaderSaysWhereTheTreeIs,
		"TheLabelPickersNoteFitsTheBox":                 checkTheLabelPickersNoteFitsTheBox,
		"TheListScrollsAheadOfTheCursor":                checkTheListScrollsAheadOfTheCursor,
		"TheListHandsALabelListingToItsPicker":          checkTheListHandsALabelListingToItsPicker,
		"ThePreviewPaneSaysHowMuchItHolds":              checkThePreviewPaneSaysHowMuchItHolds,
		"AKeyWaitsOutARebuildUnlessItOnlyMoves":         checkAKeyWaitsOutARebuildUnlessItOnlyMoves,
		"FoldAllReachesTheConversations":                checkFoldAllReachesTheConversations,
		"OverlaysScrollByTheirOwnMeasure":               checkOverlaysScrollByTheirOwnMeasure,
		"FailuresSayTheirReason":                        checkFailuresSayTheirReason,
		"RevealPlacesABlockByItsHeight":                 checkRevealPlacesABlockByItsHeight,
		"AHugeDiffIsBuiltOffTheLoop":                    checkAHugeDiffIsBuiltOffTheLoop,
		"AShutHeadIsFolded":                             checkAShutHeadIsFolded,
		"AgentMarkdownDrawsEachMark":                    checkAgentMarkdownDrawsEachMark,
		"ASplitBlockPairsInOrder":                       checkASplitBlockPairsInOrder,
		"ADefinitionCardCountsTheRest":                  checkADefinitionCardCountsTheRest,
		"APlanHeadingIsNamedOnlyWhereItSaysSomething":   checkAPlanHeadingIsNamedOnlyWhereItSaysSomething,
		"TheOtherEndOfAMoveIsTheMovedLine":              checkTheOtherEndOfAMoveIsTheMovedLine,
		"ComparingSaysWhatItHid":                        checkComparingSaysWhatItHid,
		"SearchLeavesItsPrompt":                         checkSearchLeavesItsPrompt,
		"AShapeMemberIsExactByItsOwnFile":               checkAShapeMemberIsExactByItsOwnFile,
		"AHintIsDrawnWholeOrNotAtAll":                   checkAHintIsDrawnWholeOrNotAtAll,
		"TheTourFollowsTheCursor":                       checkTheTourFollowsTheCursor,
		"TheNarrowOutlineSplitsTheFrame":                checkTheNarrowOutlineSplitsTheFrame,
		"TheStripKeepsTheNameUnderTheCursorWhileItFits": checkTheStripKeepsTheNameUnderTheCursorWhileItFits,
		"ATroubleNoteWrapsInsideTheGutter":              checkATroubleNoteWrapsInsideTheGutter,
		"TheViewAndItsInertKeys":                        checkTheViewAndItsInertKeys,
		"TroubleFilesAreSetApart":                       checkTroubleFilesAreSetApart,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t)
		})
	}
}
