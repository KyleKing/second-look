package stub_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/kyleking/aragonite/hermetic"

	"github.com/kyleking/second-look/internal/stub"
)

func TestMain(m *testing.M) { hermetic.Main(m, hermetic.App("second-look")) }

// Many tests writing a stub and running it at once, the way the suite's parallel tests do. Written
// in the test binary, one of them would sooner or later fail with "text file busy".
func TestWrite_RunsAtOnceBesideParallelForks(t *testing.T) {
	t.Parallel()

	for i := range 32 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "tool")
			stub.Write(t, path, "#!/bin/sh\necho ran "+strconv.Itoa(i)+"\n")

			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("the stub is %v, %v; want it owner-executable", info, err)
			}

			out, err := exec.CommandContext(t.Context(), path).Output() //nolint:gosec // G204: the stub just written
			if err != nil || string(out) != "ran "+strconv.Itoa(i)+"\n" {
				t.Errorf("running the stub: %q, %v", out, err)
			}
		})
	}
}
