package main_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	main "github.com/kyleking/second-look/cmd/second-look"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/lease"
)

// leaseHome points the per-repository state directory at a scratch root for a
// test running in this process, the way testHome does for a subprocess.
func leaseHome(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

// seedBlockedReview stages a review holding a draft, which is what the release
// warnings count.
func seedBlockedReview(t *testing.T, repository string, number int) {
	t.Helper()

	owner, name, _ := strings.Cut(repository, "/")

	root, err := artifact.StateRoot("github.com", owner, name)
	if err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`version = 1
host = "github.com"
owner = %q
repo = %q
number = %d
head_sha = "abc1234"

[[comment]]
id = "one"
path = "a.go"
line = 1
side = "RIGHT"
body = "not finished"
status = "draft"
`, owner, name, number)

	write(t, artifact.Path(root, number), []byte(body))
}

// The clone list C picks from adds this directory when it is a clone the
// dashboard's scan does not reach, and puts the sitting's own claim first
// since it is already held.
func TestCloneCandidatesMergesThisDirectoryAndOwnsTheClaim(t *testing.T) { //nolint:paralleltest // leaseHome sets HOME
	leaseHome(t)

	dir, _ := scratchRepo(t, "main")
	t.Chdir(dir)

	dashboard := []byte(`{"repos":[{` +
		`"path":"/clone/other","branch":"main",` +
		`"remote":"KyleKing/second-look","dirty":false}]}`)

	found, err := main.CloneCandidates(t.Context(), "KyleKing/second-look", "", dashboard)
	if err != nil {
		t.Fatal(err)
	}

	if len(found) != 2 {
		t.Fatalf("candidates = %v, want the dashboard's and this directory's", found)
	}

	h, err := lease.Acquire("github.com", "KyleKing/second-look", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()

	found, err = main.CloneCandidates(t.Context(), "KyleKing/second-look", "", dashboard)
	if err != nil {
		t.Fatal(err)
	}

	if found[0].Path != dir {
		t.Fatalf("the claimed clone ranks %v, want first", found)
	}
}

// A detached review adopts the checkout the sitting claimed, and a claim on
// another repository is left alone.
func TestLeasedWorkAnswersForTheClaimedCheckout(t *testing.T) { //nolint:paralleltest // leaseHome sets HOME
	leaseHome(t)

	dir, _ := scratchRepo(t, "main")

	detached, err := get.Away("KyleKing", "second-look", 2)
	if err != nil {
		t.Fatal(err)
	}

	if got := main.LeasedWork(t.Context(), detached); !got.Detached() {
		t.Fatalf("no claim adopted %q anyway", got.Work)
	}

	other, err := lease.Acquire("github.com", "owner/other", "/clone/elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()

	if got := main.LeasedWork(t.Context(), detached); !got.Detached() {
		t.Fatalf("another repository's claim adopted %q", got.Work)
	}

	h, err := lease.Acquire("github.com", "KyleKing/second-look", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()

	got := main.LeasedWork(t.Context(), detached)
	if got.Work != dir {
		t.Fatalf("Work = %q, want the claimed %q", got.Work, dir)
	}
}

// Focus leaving a repository hands its claim back and says so, warning when
// the repository's staged reviews still hold drafts.
func TestFocusLeavingReleasesTheClaim(t *testing.T) { //nolint:paralleltest // leaseHome sets HOME
	leaseHome(t)

	if _, err := lease.Acquire("github.com", "KyleKing/second-look", "/clone/one"); err != nil {
		t.Fatal(err)
	}

	msg, ok := main.ReleaseOn("KyleKing/second-look")
	if !ok {
		t.Fatal("releasing reported nothing to the footer")
	}

	if msg.Text != "released one" || msg.Failed {
		t.Fatalf("the footer said %q (failed %v), want a plain release", msg.Text, msg.Failed)
	}

	if lease.Ours("github.com", "KyleKing/second-look") != nil {
		t.Fatal("the claim survived focus leaving")
	}

	if _, ok := main.ReleaseOn("KyleKing/second-look"); ok {
		t.Fatal("re-releasing said something, want silence")
	}

	// A repository still holding drafts warns as the claim goes back.
	h, err := lease.Acquire("github.com", "KyleKing/second-look", "/clone/one")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()

	seedBlockedReview(t, "KyleKing/second-look", 5)

	msg, ok = main.ReleaseOn("KyleKing/second-look")
	if !ok || !msg.Failed || !strings.Contains(msg.Text, "still blocked") {
		t.Fatalf("a blocked repository released as %v, want a warning", msg)
	}
}

// Leaving the sitting hands back every claim, naming the repositories still
// holding drafts.
func TestLeavingReleasesEveryClaim(t *testing.T) { //nolint:paralleltest // leaseHome sets HOME
	leaseHome(t)

	for _, tc := range []struct{ repo, path string }{
		{"KyleKing/second-look", "/clone/one"},
		{"owner/other", "/clone/two"},
	} {
		if _, err := lease.Acquire("github.com", tc.repo, tc.path); err != nil {
			t.Fatal(err)
		}
	}

	seedBlockedReview(t, "KyleKing/second-look", 5)

	var out strings.Builder
	if err := main.ReleaseOurs(&out); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, "released /clone/one; 1 review on KyleKing/second-look still blocked") ||
		!strings.Contains(got, "released /clone/two") {
		t.Fatalf("leaving said %q", got)
	}

	if lease.Ours("github.com", "KyleKing/second-look") != nil ||
		lease.Ours("github.com", "owner/other") != nil {
		t.Fatal("a claim survived leaving")
	}
}
