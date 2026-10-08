package lease

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// AliveForTest exposes the platform liveness check to tests, which is the only
// caller besides the sweep that needs to ask it directly.
func AliveForTest(pid int) bool { return alive(pid) }

// PlantForTest writes a claim as if pid took it, so a test can stand a live
// process's lease up without spawning a process that also claims the path.
func PlantForTest(t *testing.T, host, repo, path string, pid int) {
	t.Helper()

	d, err := dir(host, repo)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(d, dirPerm); err != nil {
		t.Fatal(err)
	}

	body, err := toml.Marshal(Record{Repo: repo, Path: path, PID: pid, Acquired: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(d, key(path)), body, filePerm); err != nil {
		t.Fatal(err)
	}
}
