package local_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/local"
)

// A marker is written above its line in the file's own comment syntax, at the
// indent of the code it guards, and is refused whole where it cannot be placed:
// a file with no comment syntax, a line past the end, or nothing to say. A
// refusal leaves the file as it was.
func TestWriteTODO(t *testing.T) {
	t.Parallel()

	const src = "package x\n\nfunc f() {\n\twork()\n}\n"

	tests := []struct {
		name string
		rel  string
		at   int
		note string
		want string
		err  string
	}{
		{
			name: "go takes slashes", rel: "x/one.go", at: 2, note: "check the nil case",
			want: "package x\n// [TODO: check the nil case]\n\nfunc f() {\n\twork()\n}\n",
		},
		{
			name: "python takes a hash", rel: "x/two.py", at: 1, note: "rate limit",
			want: "# [TODO: rate limit]\n" + src,
		},
		{
			name: "markup is bracketed", rel: "x/page.html", at: 1, note: "the modal",
			want: "<!-- [TODO: the modal] -->\n" + src,
		},
		{
			name: "a long note flattens", rel: "x/one.go", at: 1, note: "one\n  two\tthree",
			want: "// [TODO: one two three]\n" + src,
		},
		{
			name: "the anchor's indent", rel: "x/one.go", at: 4, note: "handle the error",
			want: "package x\n\nfunc f() {\n\t// [TODO: handle the error]\n\twork()\n}\n",
		},
		{name: "a data file", rel: "x/one.json", at: 1, note: "note", err: local.ErrNoCommentSyntax.Error()},
		{name: "a line past the file", rel: "x/one.go", at: 40, note: "note", err: "asked for 40"},
		{name: "nothing to say", rel: "x/one.go", at: 1, note: "  ", err: " "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write(t, root, tc.rel, src)

			err := local.WriteTODO(root, tc.rel, tc.at, tc.note)
			if got := fmt.Sprint(err); (tc.err == "") != (err == nil) || !strings.Contains(got, tc.err) {
				t.Fatalf("WriteTODO answered %v, want %q", err, tc.err)
			}

			body, err := os.ReadFile(filepath.Join(root, tc.rel)) //nolint:gosec // the file this test wrote
			if err != nil {
				t.Fatal(err)
			}

			want := tc.want
			if want == "" {
				want = src
			}

			if string(body) != want {
				t.Errorf("the file reads\n%s\nwant\n%s", body, want)
			}
		})
	}
}
