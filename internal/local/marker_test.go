package local_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/local"
)

func TestWriteTODO(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rel  string
		at   int
		note string
		want string
	}{
		{"go takes slashes", "x/one.go", 2, "check the nil case", "// [TODO: check the nil case]"},
		{"python takes a hash", "x/two.py", 1, "rate limit", "# [TODO: rate limit]"},
		{"markup is bracketed", "x/page.html", 1, "the modal", "<!-- [TODO: the modal] -->"},
		{"a long note flattens", "x/one.go", 1, "one\n  two\tthree", "// [TODO: one two three]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write(t, root, tc.rel, "package x\n\nfunc f() {\n\twork()\n}\n")

			if err := local.WriteTODO(root, tc.rel, tc.at, tc.note); err != nil {
				t.Fatalf("WriteTODO: %v", err)
			}

			body, err := os.ReadFile(filepath.Join(root, tc.rel)) //nolint:gosec // the file this test wrote
			if err != nil {
				t.Fatal(err)
			}

			lines := strings.Split(string(body), "\n")
			if lines[tc.at-1] != tc.want {
				t.Errorf("line %d is %q, want the marker %q:\n%s", tc.at, lines[tc.at-1], tc.want, body)
			}
		})
	}
}

// The marker takes its line's own indent, so it reads at the depth the code
// it guards sits at.
func TestWriteTODOIndentsLikeTheAnchor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "x/one.go", "func f() {\n\twork()\n}\n")

	if err := local.WriteTODO(root, "x/one.go", 2, "handle the error"); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(root, "x", "one.go")) //nolint:gosec // the file this test wrote
	if err != nil {
		t.Fatal(err)
	}

	if lines := strings.Split(string(body), "\n"); lines[1] != "\t// [TODO: handle the error]" {
		t.Errorf("the marker did not take the anchor's indent:\n%s", body)
	}
}

func TestWriteTODORefusesWhatItCannotPlace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "x/one.go", "package x\n")

	if err := local.WriteTODO(root, "x/one.json", 1, "note"); !errors.Is(err, local.ErrNoCommentSyntax) {
		t.Errorf("a data file got %v, want ErrNoCommentSyntax", err)
	}

	err := local.WriteTODO(root, "x/one.go", 40, "note")
	if err == nil || !strings.Contains(err.Error(), "asked for 40") {
		t.Errorf("a line past the file got %v", err)
	}

	if err := local.WriteTODO(root, "x/one.go", 1, "  "); err == nil {
		t.Error("an empty note landed a marker anyway")
	}
}
