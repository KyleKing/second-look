// Package stub writes the executables tests put in a program's way: a stand-in gh on PATH, an
// $EDITOR, a project's own linter.
//
// A test that writes a script and then runs it races every parallel test that starts a process:
// a child forked while the script is open for writing inherits that descriptor until it execs, and
// running the script meanwhile fails with "text file busy" (golang/go#22315). Write hands the
// writing to a shell of its own, so the descriptor never exists in the test binary and no sibling's
// child can hold it.
package stub

import (
	"os/exec"
	"strings"
	"testing"
)

// Write makes path an executable holding script, owner-only, and fails tb when it cannot.
func Write(tb testing.TB, path, script string) {
	tb.Helper()

	//nolint:gosec // G204: a fixed script; the path is the caller's own temp file, passed as an argument
	cmd := exec.CommandContext(tb.Context(), "sh", "-c", `cat > "$1" && chmod 700 "$1"`, "sh", path)
	cmd.Stdin = strings.NewReader(script)

	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("writing the stub %s: %v\n%s", path, err, out)
	}
}
