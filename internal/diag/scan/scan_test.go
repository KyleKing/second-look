package scan_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diag/scan"
)

// The fixtures are what the real programs printed, kept because the counting is
// the part worth pinning and neither program documents it: ast-grep numbers its
// lines from zero and ruff from one, so a parser that read either the other way
// would put every note one line off and still look right. Only the absolute
// path in ruff's answer is rewritten, since a recording of this laptop's
// scratch directory would say nothing anywhere else.
func TestReadCountsLinesTheWayEachProgramPrintsThem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		file   string
		format scan.Format
		root   string
		want   diag.Note
	}{
		{
			name: "ast-grep counts from zero", file: "ast-grep.golden",
			format: scan.AstGrep, root: "/work",
			want: diag.Note{
				Path: "src/x.go", Line: 3, End: 3, Source: "house rules",
				Code: "long-comment", Message: "a comment block runs past two lines",
				Severity: diag.Warning,
			},
		},
		{
			name: "ruff counts from one and answers absolute", file: "ruff.golden",
			format: scan.Ruff, root: "/repo",
			want: diag.Note{
				Path: "t.py", Line: 1, End: 1, Source: "house rules", Code: "F401",
				Message: "`os` imported but unused", Severity: diag.Warning,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}

			got, err := scan.Read(tc.format, "house rules", tc.root, raw)
			if err != nil {
				t.Fatalf("reading %s: %v", tc.format, err)
			}

			if len(got) == 0 {
				t.Fatalf("%s answered nothing", tc.format)
			}

			if got[0] != tc.want {
				t.Errorf("first note is\n  %+v\nwant\n  %+v", got[0], tc.want)
			}
		})
	}
}

// The text format is what every other linter can be asked for, and the one
// shape it has to survive is a path carrying a colon of its own.
func TestReadTextFindsTheLineNumberPastAColonInThePath(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"internal/a.go:12:4: a thing is wrong",
		"C:/work/b.go:3: another thing",
		"not a diagnostic at all",
		"internal/c.go:no number: ignored",
	}, "\n")

	got, err := scan.Read(scan.Text, "golangci-lint", "/work", []byte(out))
	if err != nil {
		t.Fatalf("reading text: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("read %d notes from\n%s\nwant 2", len(got), out)
	}

	if got[0].Line != 12 || got[0].Message != "a thing is wrong" {
		t.Errorf("first note is %+v", got[0])
	}

	if got[1].Path != "C:/work/b.go" || got[1].Line != 3 {
		t.Errorf("a path carrying a colon read as %+v", got[1])
	}
}

// A checker that found something exits non-zero, which is the same status one
// that could not run exits with. Output is what tells them apart, and a run
// that confused the two would either drop every finding or report every clean
// pass as broken.
func TestRunReadsAFailingCheckerThatPrintedAnAnswer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	found := scan.Check{
		Name:    "picky",
		Command: []string{"sh", "-c", `echo "$1:4: no"; exit 1`, "sh", "{files}"},
		Format:  scan.Text,
	}

	got, err := scan.Run(t.Context(), root, []scan.Check{found}, []string{"a.go"})
	if err != nil {
		t.Fatalf("running a checker that found something: %v", err)
	}

	if len(got) != 1 || got[0].Path != "a.go" || got[0].Line != 4 {
		t.Fatalf("read %+v, want one note on a.go:4", got)
	}
}

func TestRunReportsACheckerThatCouldNotRun(t *testing.T) {
	t.Parallel()

	broken := scan.Check{
		Name:    "missing",
		Command: []string{"sh", "-c", "echo boom >&2; exit 2"},
		Format:  scan.Text,
	}

	got, err := scan.Run(t.Context(), t.TempDir(), []scan.Check{broken}, nil)
	if err == nil {
		t.Fatalf("a checker that printed nothing and failed read as %+v", got)
	}

	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("the failure does not carry what the checker said: %v", err)
	}
}

// A command asking for the review's files and given none has nothing to check.
// Running it anyway would check the whole project, which is the one answer a
// review must not silently be given.
func TestRunSkipsAFileCommandWithNoFiles(t *testing.T) {
	t.Parallel()

	ran := filepath.Join(t.TempDir(), "ran")
	check := scan.Check{
		Name:    "would run",
		Command: []string{"sh", "-c", "touch " + ran, "sh", "{files}"},
		Format:  scan.Text,
	}

	if _, err := scan.Run(context.Background(), t.TempDir(), []scan.Check{check}, nil); err != nil {
		t.Fatalf("running with no files: %v", err)
	}

	if _, err := os.Stat(ran); err == nil {
		t.Error("the command ran with no files to check")
	}
}
