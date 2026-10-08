package lease_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/lease"
)

const host = "github.com"

// testHome points the state directory at a scratch root, which is where a
// test's claims must live to not touch the real one's.
func testHome(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	if runtime.GOOS != "windows" {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	}
}

// helperProcess is the child a liveness test borrows a pid from: it blocks on
// stdin until the test kills it, so its pid answers alive() honestly.
func TestHelperProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv("LEASE_HELPER") != "1" {
		return
	}

	if _, err := os.Stdin.Read(make([]byte, 1)); err != nil {
		os.Exit(0)
	}

	os.Exit(0)
}

// spawnPID starts a helper holding a live pid, and reports how to end it.
func spawnPID(t *testing.T) (int, func()) {
	t.Helper()

	//nolint:gosec // helper-process re-exec
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(), "LEASE_HELPER=1")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	return cmd.Process.Pid, func() {
		if err := stdin.Close(); err != nil {
			t.Errorf("closing the helper's stdin: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Errorf("reaping the helper: %v", err)
		}
	}
}

// deadPID returns a pid that belonged to a process, since a stale lease read
// is only stale against a pid that ran and exited.
func deadPID(t *testing.T) int {
	t.Helper()

	pid, done := spawnPID(t)
	done()

	// A just-exited pid can take a moment to register dead on some systems.
	for range 50 {
		if !lease.AliveForTest(pid) {
			return pid
		}

		time.Sleep(10 * time.Millisecond)
	}

	return pid
}

func TestAcquireRelease(t *testing.T) { //nolint:paralleltest // testHome sets HOME, which parallel tests share
	testHome(t)

	h, err := lease.Acquire(host, "owner/repo", "/clone/one")
	if err != nil {
		t.Fatal(err)
	}

	if got := lease.Ours(host, "owner/repo"); got == nil || got.Record.Path != "/clone/one" {
		t.Fatalf("Ours = %v, want /clone/one", got)
	}

	h.Release()
	h.Release() // releasing twice is not an error

	if got := lease.Ours(host, "owner/repo"); got != nil {
		t.Fatalf("Ours after release = %v, want nil", got)
	}
}

func TestAcquire_Taken(t *testing.T) { //nolint:paralleltest // testHome sets HOME, which parallel tests share
	testHome(t)

	pid, done := spawnPID(t)
	defer done()

	lease.PlantForTest(t, host, "owner/repo", "/clone/one", pid)

	_, err := lease.Acquire(host, "owner/repo", "/clone/one")

	var taken *lease.TakenError
	if !errors.As(err, &taken) {
		t.Fatalf("Acquire = %v, want Taken", err)
	}

	if taken.Record.Path != "/clone/one" {
		t.Fatalf("Taken names %q, want /clone/one", taken.Record.Path)
	}
}

func TestAcquire_SweepsStale(t *testing.T) { //nolint:paralleltest // testHome sets HOME, which parallel tests share
	testHome(t)

	lease.PlantForTest(t, host, "owner/repo", "/clone/one", deadPID(t))

	h, err := lease.Acquire(host, "owner/repo", "/clone/one")
	if err != nil {
		t.Fatalf("a dead pid's claim should be swept: %v", err)
	}
	defer h.Release()

	if got := lease.Ours(host, "owner/repo"); got == nil {
		t.Fatal("Ours = nil, want the claim")
	}
}

func TestList_SkipsStaleAndKeysByPath(t *testing.T) { //nolint:paralleltest // testHome sets HOME
	testHome(t)

	lease.PlantForTest(t, host, "owner/repo", "/clone/dead", deadPID(t))

	h, err := lease.Acquire(host, "owner/repo", "/clone/one")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()

	got := lease.List(host, "owner/repo")
	if len(got) != 1 {
		t.Fatalf("List = %v, want the live claim alone", got)
	}

	rec, ok := got["/clone/one"]
	if !ok {
		t.Fatalf("List = %v, want /clone/one keyed", got)
	}

	if rec.PID != os.Getpid() {
		t.Fatalf("claim carries pid %d, want %d", rec.PID, os.Getpid())
	}
}

func TestReleaseAll(t *testing.T) { //nolint:paralleltest // testHome sets HOME, which parallel tests share
	testHome(t)

	for _, tc := range []struct{ repo, path string }{
		{"owner/repo", "/clone/one"},
		{"owner/other", "/clone/two"},
	} {
		if _, err := lease.Acquire(host, tc.repo, tc.path); err != nil {
			t.Fatal(err)
		}
	}

	got, err := lease.ReleaseAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("ReleaseAll returned %v, want both paths", got)
	}

	if len(lease.List(host, "owner/repo")) != 0 {
		t.Fatal("a claim survived ReleaseAll")
	}
}

func TestAcquire_BadRepo(t *testing.T) { //nolint:paralleltest // testHome sets HOME, which parallel tests share
	testHome(t)

	if _, err := lease.Acquire(host, "nonsense", "/clone/one"); err == nil {
		t.Fatal("a repo without owner/name acquired a claim")
	}
}
