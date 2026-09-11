package main_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	main "github.com/kyleking/second-look/cmd/second-look"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // constants from this function
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=second-look", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=second-look", "GIT_COMMITTER_EMAIL=test@example.com")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

// featureCheckout is a repository on main with a feature branch checked out
// one commit ahead, which is what a reviewed pull request's checkout looks
// like: a base branch to return to, and a head to leave once done with it.
func featureCheckout(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	gitIn(t, dir, "init", "--quiet", "--initial-branch", "main")
	gitIn(t, dir, "commit", "--quiet", "--allow-empty", "-m", "base")
	gitIn(t, dir, "checkout", "--quiet", "-b", "feature")
	gitIn(t, dir, "commit", "--quiet", "--allow-empty", "-m", "feature work")

	return dir, gitIn(t, dir, "rev-parse", "HEAD")
}

// Deleting the branch the review was staged against is exactly the case the
// shortcut exists for: the checkout is left on main, and the branch is gone.
func TestCleanerSwitchesAndDeletesWhenTheCheckoutMatchesTheReview(t *testing.T) {
	t.Parallel()

	dir, head := featureCheckout(t)
	target := get.Target{Owner: "kyleking", Repo: "second-look", Number: 1, Work: dir, Store: dir}
	r := &artifact.Review{HeadRef: "feature", BaseRef: "main", HeadSHA: head}

	summary, err := main.DeleteBranchLocally(target)(t.Context(), r)
	if err != nil {
		t.Fatalf("Cleaner() = %v, want nil", err)
	}

	if !strings.Contains(summary, "feature") || !strings.Contains(summary, "main") {
		t.Errorf("summary = %q, want it to name both branches", summary)
	}

	if got := gitIn(t, dir, "branch", "--show-current"); got != "main" {
		t.Errorf("checked out %q, want main", got)
	}

	branches := gitIn(t, dir, "branch", "--list", "feature")
	if branches != "" {
		t.Errorf("feature still exists: %q", branches)
	}
}

// A checkout that moved past what the review names is not second-look's to
// discard: it might carry commits the reviewer added on top.
func TestCleanerRefusesWhenTheCheckoutHasMovedOn(t *testing.T) {
	t.Parallel()

	dir, head := featureCheckout(t)
	gitIn(t, dir, "commit", "--quiet", "--allow-empty", "-m", "one more, after the review was staged")

	target := get.Target{Owner: "kyleking", Repo: "second-look", Number: 1, Work: dir, Store: dir}
	r := &artifact.Review{HeadRef: "feature", BaseRef: "main", HeadSHA: head}

	_, err := main.DeleteBranchLocally(target)(t.Context(), r)
	if err == nil {
		t.Fatal("Cleaner() = nil, want a refusal")
	}

	if got := gitIn(t, dir, "branch", "--show-current"); got != "feature" {
		t.Errorf("checked out %q despite the refusal, want feature left alone", got)
	}
}
