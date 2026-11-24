package ghrun_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/stub"
)

// fakeGH answers by its first argument: "ok" prints the directory it ran in and
// its arguments, "fail" explains itself on stderr, and "mute" exits without a word.
const fakeGH = `#!/bin/sh
case "$1" in
ok) echo "$(pwd) $*" ;;
fail) echo "  HTTP 404: Not Found  " >&2; exit 1 ;;
*) exit 2 ;;
esac
`

// TestOutputCarriesGhsOwnReason holds a failure to gh's stderr, since the exit
// status alone says nothing, and names only the first two arguments of the call.
// It sets PATH, so it cannot run in parallel.
func TestOutputCarriesGhsOwnReason(t *testing.T) {
	bin := t.TempDir()
	stub.Write(t, filepath.Join(bin, "gh"), fakeGH)

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	out, err := ghrun.Output(t.Context(), root, "ok", "graphql", "--input", "-")
	if err != nil || string(out) != root+" ok graphql --input -\n" {
		t.Errorf("a call that worked answered %q (%v)", out, err)
	}

	if err := ghrun.GH().Run(t.Context(), root, "ok"); err != nil {
		t.Errorf("Run of a call that worked answered %v", err)
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"fail", "graphql", "-f", "query=mutation{}"}, "gh fail graphql: exit status 1: HTTP 404: Not Found"},
		{[]string{"mute"}, "gh mute: exit status 2"},
	} {
		if _, err := ghrun.Output(t.Context(), root, tc.args...); err == nil || err.Error() != tc.want {
			t.Errorf("%v answered %v, want %q", tc.args, err, tc.want)
		}

		if err := ghrun.GH().Run(t.Context(), root, tc.args...); err == nil || err.Error() != tc.want {
			t.Errorf("Run %v answered %v, want %q", tc.args, err, tc.want)
		}
	}
}
