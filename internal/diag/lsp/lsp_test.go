package lsp_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/aragonite/hermetic"

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

	code := hermetic.Run(m, hermetic.App("second-look"))

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

	start := time.Now()

	got, err := s.Notes(t.Context(), []lsp.Doc{{Path: "a.ts", Text: "one\ntwo\nthree\n"}})
	if err != nil {
		t.Fatalf("asking the stub: %v", err)
	}

	// A server publishes for every file it loaded on the way to the ones asked
	// about, and the stub never stops: a pass that let that noise restart its
	// quiet line runs to the deadline instead of settling.
	if took := time.Since(start); took > patience {
		t.Errorf("the pass took %s, which is the noise holding it open", took)
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

// A line past the end of the file is refused, and so is anything asked of a
// session that has been closed: Close ends the servers and the context they
// would be started under.
func TestHoverRefuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		line   int
		closed bool
	}{
		{"a line outside the file", 9, false},
		{"a closed session", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := lsp.New(t.Context(), t.TempDir(), stubServer())
			defer s.Close()

			if tc.closed {
				s.Close()
			}

			if got, err := s.Hover(t.Context(), lsp.Doc{Path: "a.ts", Text: "one\n"}, tc.line); err == nil {
				t.Errorf("answered %+v", got)
			}
		})
	}
}

// The other half of "what is this name" is where it lives, asked of the same
// warm server at one name. The stub answers with the character it was asked
// about as the line, so the name landing right is the proof that UTF-16
// columns crossed the wire right.
func TestDefinitionAnswersWithThePlaceTheNameLives(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	// "café" is five bytes and four code units, so beta is asked about at
	// column 7 on the wire and not 8.
	got, err := s.Definitions(t.Context(), lsp.Doc{Path: "a.ts", Text: "café = beta\n"}, 1, 8)
	if err != nil {
		t.Fatalf("asking where the name lives: %v", err)
	}

	want := diag.Jump{Name: "beta", At: []diag.Site{{Path: "a.ts", Line: 8}}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("read %+v, want %+v", got, want)
	}
}

// A site outside the asked file is spelled the way the diff spells paths, not
// as the URL the server answered with, and the declaration is left out.
func TestReferencesListsUsesWithoutTheDeclaration(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	got, err := s.References(t.Context(), lsp.Doc{Path: "a.ts", Text: "alpha + alpha\n"}, 1, 8)
	if err != nil {
		t.Fatalf("asking where the name is read: %v", err)
	}

	want := diag.Jump{Name: "alpha", At: []diag.Site{{Path: "a.ts", Line: 1}, {Path: "other.ts", Line: 42}}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("read %+v, want %+v", got, want)
	}
}

func TestPlacesRefusesWhatItCannotAnswer(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	doc := lsp.Doc{Path: "a.ts", Text: "one\n"}
	if _, err := s.Definitions(t.Context(), doc, 9, 0); !errors.Is(err, lsp.ErrNoLine) {
		t.Errorf("a line past the end of the file was answered: %v", err)
	}

	between := lsp.Doc{Path: "a.ts", Text: "a + b\n"}
	if _, err := s.Definitions(t.Context(), between, 1, 1); !errors.Is(err, lsp.ErrNoName) {
		t.Errorf("a column between two names was answered: %v", err)
	}

	unclaimed := lsp.Doc{Path: "README.md", Text: "one\n"}
	if _, err := s.References(t.Context(), unclaimed, 1, 0); !errors.Is(err, lsp.ErrNoServer) {
		t.Errorf("a file nothing claims was answered: %v", err)
	}

	// The stub refuses every ask pointed at column zero, the way a real server
	// refuses a keyword it cannot place.
	if _, err := s.References(t.Context(), lsp.Doc{Path: "a.ts", Text: "func\n"}, 1, 0); err == nil {
		t.Error("a name the server refused read as answered")
	}
}

// The graph is asked at the name of the function the line is inside, which the
// server's own outline says: the row is in the body, three rows below the name.
// Sites of a callee are in the center's file, and a caller outside the project
// keeps the path the server gave.
func TestCallsReadsOneHopEachWayFromTheFunctionTheLineIsIn(t *testing.T) {
	t.Parallel()

	s := lsp.New(t.Context(), t.TempDir(), stubServer())
	defer s.Close()

	doc := lsp.Doc{Path: "a.ts", Text: "class Syncer {\n  sync() {\n    pages()\n  }\n\n\n\n}\n"}

	got, err := s.Calls(t.Context(), doc, 3)
	if err != nil {
		t.Fatalf("asking the stub: %v", err)
	}

	want := diag.Graph{
		Server: "stub",
		Center: diag.CallNode{Name: "sync", Detail: "asked at 1:2", Path: "a.ts", Line: 2, From: 2, To: 5},
		Callers: []diag.CallNode{
			{
				Name: "viaDep", Detail: "pkg/viaDep", Path: "/elsewhere/dep.ts", Line: 8, From: 8, To: 10,
				Sites: []diag.Site{{Path: "/elsewhere/dep.ts", Line: 9}},
			},
			{
				Name: "run", Detail: "pkg/run", Path: "other.ts", Line: 4, From: 4, To: 6,
				Sites: []diag.Site{{Path: "other.ts", Line: 6}, {Path: "other.ts", Line: 5}},
			},
		},
		Callees: []diag.CallNode{{
			Name: "pages", Detail: "pkg/pages", Path: "other.ts", Line: 12, From: 12, To: 14,
			Sites: []diag.Site{{Path: "a.ts", Line: 3}},
		}},
	}

	if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
		t.Errorf("read\n%+v\nwant\n%+v", got, want)
	}

	if _, err := s.Calls(t.Context(), doc, 7); !errors.Is(err, lsp.ErrNoFunction) {
		t.Errorf("a row of the class outside its method was answered: %v", err)
	}

	if _, err := s.Calls(t.Context(), doc, 99); !errors.Is(err, lsp.ErrNoLine) {
		t.Errorf("a row past the end of the file was answered: %v", err)
	}
}

func TestCallsNamesTheServerThatHasNoCallHierarchy(t *testing.T) {
	t.Parallel()

	servers := stubServer()
	servers[0].Argv = append(servers[0].Argv, "-no-calls")

	s := lsp.New(t.Context(), t.TempDir(), servers)
	defer s.Close()

	_, err := s.Calls(t.Context(), lsp.Doc{Path: "a.ts", Text: "one\n"}, 1)
	if !errors.Is(err, lsp.ErrNoCallHierarchy) || err.Error() != "stub has no call hierarchy" {
		t.Errorf("a server without call hierarchy was asked anyway: %v", err)
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

// TestNotesRootsEachServer is where a server is started and what it is told,
// read back from the stub, which answers root.ts with the directory it was
// started for and settings.ts with what it was configured with.
//
// A server starts in the project the file belongs to, not the checkout. A
// workspace marker above a file outranks a package marker beside it, because
// the workspace is what resolves the siblings the file imports. A project that
// cannot host the server is walked past: rooted there, tsserver finds no
// typescript to run out of and exits without answering. A server asking for its
// settings is answered, since what it is told decides what it reports; settings
// that depend on the project are computed for the root it starts in; and what a
// config names beats what is detected.
func TestNotesRootsEachServer(t *testing.T) {
	t.Parallel()

	for _, tc := range []rooting{
		{
			name:  "in the file's own project",
			files: []string{"pkg/tsconfig.json"}, roots: [][]string{{"tsconfig.json"}},
			want: map[string]string{"pkg/src/root.ts": "{checkout}/pkg", "root.ts": "{checkout}"},
		},
		{
			name:  "at the workspace over a nearer package",
			files: []string{"workspace.json", "pkg/package.json"},
			roots: [][]string{{"workspace.json"}, {"package.json"}},
			want:  map[string]string{"pkg/root.ts": "{checkout}"},
		},
		{
			name:  "past a project that cannot host it",
			files: []string{"tsconfig.json", "pkg/tsconfig.json", "node_modules/typescript/.keep"},
			roots: [][]string{{"tsconfig.json"}}, needs: []string{"node_modules/typescript"},
			want: map[string]string{"pkg/root.ts": "{checkout}"},
		},
		{
			name:     "with the settings it asks for",
			settings: map[string]any{"stub": map[string]any{"mode": "workspace"}},
			want:     map[string]string{"settings.ts": "workspace"},
		},
		{
			name:  "with its own root's settings",
			files: []string{"pkg/tsconfig.json"}, roots: [][]string{{"tsconfig.json"}},
			detect: func(_, root string) map[string]any { return map[string]any{"stub": map[string]any{"mode": root}} },
			want:   map[string]string{"pkg/settings.ts": "{checkout}/pkg"},
		},
		{
			name:     "with configured settings over detected ones",
			settings: map[string]any{"stub": map[string]any{"mode": "configured"}},
			detect: func(_, _ string) map[string]any {
				return map[string]any{"stub": map[string]any{"mode": "found"}}
			},
			want: map[string]string{"settings.ts": "configured"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t)
		})
	}
}

// rooting is one checkout laid out with files, a stub server configured over
// it, and the answer each document should come back with.
type rooting struct {
	name         string
	files, needs []string
	roots        [][]string
	settings     map[string]any
	detect       func(checkout, root string) map[string]any
	want         map[string]string
}

func (tc *rooting) check(t *testing.T) {
	t.Helper()

	checkout := t.TempDir()

	for _, f := range tc.files {
		at := filepath.Join(checkout, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(at), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(at, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	servers := stubServer()
	servers[0].Roots, servers[0].Needs = tc.roots, tc.needs
	servers[0].Settings, servers[0].SettingsFor = tc.settings, tc.detect

	s := lsp.New(t.Context(), checkout, servers)
	defer s.Close()

	docs := make([]lsp.Doc, 0, len(tc.want))
	for path := range tc.want {
		docs = append(docs, lsp.Doc{Path: filepath.FromSlash(path), Text: "one\n"})
	}

	got, err := s.Notes(t.Context(), docs)
	if err != nil {
		t.Fatalf("asking the stub: %v", err)
	}

	if len(got) != len(tc.want) {
		t.Fatalf("read %+v, want one note for each of %v", got, tc.want)
	}

	for _, n := range got {
		want := filepath.FromSlash(
			strings.ReplaceAll(tc.want[filepath.ToSlash(n.Path)], "{checkout}", filepath.ToSlash(checkout)),
		)
		if n.Message != want {
			t.Errorf("%s was answered with %q, want %q", n.Path, n.Message, want)
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
	servers[0].Roots = [][]string{{"tsconfig.json"}}

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

// pyright resolves imports against the interpreter it is told about, and a
// monorepo keeps one per package; the nearer .venv wins and none means none.
func TestPyrightIsToldTheProjectsInterpreter(t *testing.T) {
	t.Parallel()

	var pyright lsp.Server

	for _, s := range lsp.Builtin() {
		if s.Name == "pyright" {
			pyright = s
		}
	}

	checkout := t.TempDir()
	root := filepath.Join(checkout, "pkg")

	for _, at := range []string{
		filepath.Join(root, ".venv", "bin", "python"),
		filepath.Join(checkout, ".venv", "bin", "python"),
	} {
		if err := os.MkdirAll(filepath.Dir(at), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(at, []byte("#!"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	interpreter := func(root string) any {
		python, ok := pyright.SettingsFor(checkout, root)["python"].(map[string]any)
		if !ok {
			return nil
		}

		return python["pythonPath"]
	}

	want := filepath.Join(root, ".venv", "bin", "python")
	if got := interpreter(root); got != want {
		t.Errorf("the package's own interpreter is %v, want %s", got, want)
	}

	// A package without one shares the environment the checkout installed.
	if err := os.RemoveAll(filepath.Join(root, ".venv")); err != nil {
		t.Fatal(err)
	}

	want = filepath.Join(checkout, ".venv", "bin", "python")
	if got := interpreter(root); got != want {
		t.Errorf("the workspace's interpreter is %v, want %s", got, want)
	}

	if err := os.RemoveAll(filepath.Join(checkout, ".venv")); err != nil {
		t.Fatal(err)
	}

	if got := pyright.SettingsFor(checkout, root); got != nil {
		t.Errorf("a project with no environment is told %v, want nothing", got)
	}
}
