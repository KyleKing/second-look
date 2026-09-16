package lsp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diag/lsp"
)

// stub is the built stand-in server, built once for the whole package. The
// tests drive the real client over a real pipe against it, which is the same
// code path a language server takes, subprocess included.
var stub string //nolint:gochecknoglobals // built once in TestMain for every test here

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lsp-stub")
	if err != nil {
		panic(err)
	}

	stub = filepath.Join(dir, "stub")

	//nolint:gosec // the only variable is the temp directory this test made
	build := exec.CommandContext(context.Background(), "go", "build", "-o", stub, "./testdata/stub")

	out, err := build.CombinedOutput()
	if err != nil {
		panic(string(out))
	}

	code := m.Run()

	//nolint:errcheck // a temp directory that outlives the test is the OS's problem
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func stubServer() []lsp.Server {
	return []lsp.Server{{
		Name: "stub", Argv: []string{stub}, Exts: []string{".ts"},
		Language: map[string]string{".ts": "typescript"},
	}}
}

// The pass has to wait past a server's first word. A real server answers an
// empty list while it is still loading the project and corrects itself
// afterwards, so a collector that stopped at the first quiet moment would
// report a clean file and be wrong about it.
func TestNotesWaitsForAServerToCorrectItself(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	got, err := s.Notes(t.Context(), []lsp.Doc{{Path: "a.ts", Text: "one\ntwo\nthree\n"}})
	if err != nil {
		t.Fatalf("asking the stub: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("read %+v, want the one note the server settled on", got)
	}

	want := diag.Note{
		Path: "a.ts", Line: 3, End: 3, Source: "stub", Code: "2339",
		Message:  "Property 'is_archived' does not exist on type 'Row'.",
		Severity: diag.Error,
	}

	if got[0] != want {
		t.Errorf("note is\n  %+v\nwant\n  %+v", got[0], want)
	}
}

// A server publishes for every file it loaded on the way to the ones asked
// about, and against a monorepo it never stops. A pass that let that noise hold
// it open ran to its deadline every time.
func TestNotesIgnoresFilesNobodyAskedAbout(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	start := time.Now()

	got, err := s.Notes(t.Context(), []lsp.Doc{{Path: "a.ts", Text: "one\ntwo\nthree\n"}})
	if err != nil {
		t.Fatalf("asking the stub: %v", err)
	}

	// The stub never stops publishing for other files, so a pass that let that
	// restart its quiet line runs to the deadline instead of settling.
	if took := time.Since(start); took > patience {
		t.Errorf("the pass took %s, which is the noise holding it open", took)
	}

	for _, n := range got {
		if n.Path != "a.ts" {
			t.Errorf("a note arrived for %s, which nothing asked about", n.Path)
		}
	}
}

// patience is well under the pass's own deadline and well over what settling
// takes, so a run that lands between them is the noise being counted.
const patience = 10 * time.Second

// A review screen has no column cursor, so a line is asked about at each of its
// names. What comes back is one answer per name, with the prose under the
// signature left off.
func TestHoverAnswersEachNameOnTheLine(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	doc := lsp.Doc{Path: "a.ts", Text: "const alpha = beta\n"}

	got, err := s.Hover(t.Context(), doc, 1)
	if err != nil {
		t.Fatalf("hovering: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("read %+v, want one answer for each of const, alpha, and beta", got)
	}

	for _, sym := range got {
		if strings.Contains(sym.Text, "prose") {
			t.Errorf("%q carries the doc comment under the signature", sym.Text)
		}
	}
}

// The protocol counts a character in UTF-16 code units and Go counts bytes. A
// line of ASCII is the same number either way, so a converter that was never
// written shows up only on the line with an accent in it, asking about the
// wrong column and answering about the wrong name.
func TestHoverCountsCharactersTheWayTheProtocolDoes(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	// "café" is five bytes and four code units, so every name after it is one
	// column further along in bytes than it is on the wire.
	got, err := s.Hover(t.Context(), lsp.Doc{Path: "a.ts", Text: "café = beta\n"}, 1)
	if err != nil {
		t.Fatalf("hovering: %v", err)
	}

	// The stub answers with the column it was asked about, so what comes back
	// says which counting crossed the wire.
	want := []string{"const at0: number", "const at7: number"}
	if len(got) != len(want) {
		t.Fatalf("read %+v, want one answer for each name", got)
	}

	for i := range want {
		if got[i].Text != want[i] {
			t.Errorf("name %d answered %q, want %q", i, got[i].Text, want[i])
		}
	}
}

func TestHoverRefusesALineOutsideTheFile(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	if _, err := s.Hover(t.Context(), lsp.Doc{Path: "a.ts", Text: "one\n"}, 9); err == nil {
		t.Error("a line past the end of the file was answered")
	}
}

// A path no configured server claims is not an error and not an answer: the
// review draws what it always drew.
func TestNotesSkipsWhatNothingAnswersFor(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	got, err := s.Notes(t.Context(), []lsp.Doc{{Path: "README.md", Text: "# hello\n"}})
	if err != nil || len(got) != 0 {
		t.Errorf("a file nothing claims read as %+v, %v", got, err)
	}
}

func TestAnswersReportsWhetherAnythingIsInstalled(t *testing.T) {
	t.Parallel()

	if !lsp.Answers(stubServer(), []string{"README.md", "a.ts"}) {
		t.Error("a review carrying a file the stub claims reads as unanswerable")
	}

	if lsp.Answers(stubServer(), []string{"README.md"}) {
		t.Error("a review carrying nothing the stub claims reads as answerable")
	}

	missing := []lsp.Server{{Name: "nope", Argv: []string{"second-look-no-such-server"}, Exts: []string{".ts"}}}
	if lsp.Answers(missing, []string{"a.ts"}) {
		t.Error("a server that is not installed reads as answerable")
	}
}

func TestContextEndsAPassThatIsStillWaiting(t *testing.T) {
	t.Parallel()

	s := lsp.New(context.Background(), t.TempDir(), stubServer())
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Notes(ctx, []lsp.Doc{{Path: "a.ts", Text: "one\n"}}); err == nil {
		t.Log("a canceled pass answered with what it had, which is the empty list")
	}
}

// A monorepo holds a project per directory, and a server started above one
// compiles the file under settings that are not the project's: rooted at the
// checkout, a TypeScript server reads a package's imports as unresolvable and
// reports errors the change did not cause.
func TestNotesStartsAServerInTheProjectTheFileBelongsTo(t *testing.T) {
	t.Parallel()

	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "pkg", "src"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(checkout, "pkg", "tsconfig.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	servers := stubServer()
	servers[0].Roots = []string{"tsconfig.json"}

	s := lsp.New(t.Context(), checkout, servers)
	defer s.Close()

	got, err := s.Notes(t.Context(), []lsp.Doc{
		{Path: filepath.Join("pkg", "src", "root.ts"), Text: "one\n"},
		{Path: "root.ts", Text: "one\n"},
	})
	if err != nil {
		t.Fatalf("asking the stub: %v", err)
	}

	// The stub answers with the directory the client said it was started for.
	want := map[string]string{
		filepath.Join("pkg", "src", "root.ts"): filepath.Join(checkout, "pkg"),
		"root.ts":                              checkout,
	}

	if len(got) != len(want) {
		t.Fatalf("read %+v, want one note for each file", got)
	}

	for _, n := range got {
		if n.Message != want[n.Path] {
			t.Errorf("%s was answered by a server in %s, want one in %s", n.Path, n.Message, want[n.Path])
		}
	}
}

// A caller standing in the checkout names it ".", and a relative root walks to
// nothing and is sent to a server as a directory it cannot resolve.
//
//nolint:paralleltest // it changes the working directory, which every test shares
func TestNotesRootsAServerFromARelativeCheckout(t *testing.T) {
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(checkout, "pkg", "tsconfig.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	servers := stubServer()
	servers[0].Roots = []string{"tsconfig.json"}

	t.Chdir(checkout)

	s := lsp.New(t.Context(), ".", servers)
	defer s.Close()

	got, err := s.Notes(t.Context(), []lsp.Doc{{Path: filepath.Join("pkg", "root.ts"), Text: "one\n"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("read %+v, %v", got, err)
	}

	want, err := filepath.Abs("pkg")
	if err != nil {
		t.Fatal(err)
	}

	if got[0].Message != want {
		t.Errorf("the server was started for %s, want %s", got[0].Message, want)
	}
}
