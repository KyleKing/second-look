package get_test

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/forge"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
)

// gitIn runs git for real against dir, the way second-look's other checkout
// tests do: only gh is ever replayed, so a divergence has to be built out of
// an actual rewritten history rather than a stubbed command.
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

// divergedCheckout builds a bare origin and a clone of it, then rewrites the
// branch on the origin the way a rebase or force-push does: the clone's HEAD
// stays on the commit that is no longer reachable from the branch's new tip,
// and neither commit is an ancestor of the other.
func divergedCheckout(t *testing.T) (string, string, string, string) {
	t.Helper()

	origin := t.TempDir()
	gitIn(t, origin, "init", "--quiet", "--bare")

	seed := t.TempDir()
	gitIn(t, seed, "init", "--quiet", "--initial-branch", "feature")
	gitIn(t, seed, "commit", "--quiet", "--allow-empty", "-m", "base")
	gitIn(t, seed, "remote", "add", "origin", origin)
	gitIn(t, seed, "push", "--quiet", "origin", "feature")

	work := t.TempDir()
	gitIn(t, work, "clone", "--quiet", "--branch", "feature", origin, work)

	oldSHA := gitIn(t, work, "rev-parse", "HEAD")

	// Amend on a second clone and force-push, exactly what a colleague's
	// rebase leaves an existing checkout facing: a new commit with no path
	// back to the one the checkout is still on.
	gitIn(t, seed, "commit", "--quiet", "--amend", "--allow-empty", "-m", "base, rebased")
	gitIn(t, seed, "push", "--quiet", "--force", "origin", "feature")

	newSHA := gitIn(t, origin, "rev-parse", "feature")

	return work, "feature", oldSHA, newSHA
}

func seedRoundAt(t *testing.T, store string, number int, sha string) {
	t.Helper()

	review, err := artifact.LoadOrNew(artifact.Path(store, number))
	if err != nil {
		t.Fatal(err)
	}

	review.Version = artifact.SchemaVersion
	review.Owner, review.Repo, review.Number = "kyleking", "second-look", number
	review.Rounds = []artifact.Round{{SHA: sha}}

	if err := artifact.Save(artifact.Path(store, number), review); err != nil {
		t.Fatal(err)
	}
}

// A checkout second-look itself last left on the pre-rebase commit (its own
// Round) is safe to reset without asking: nothing but second-look's own prior
// get put it there.
func TestCheckoutRecoversFromADivergedUpstreamItStaged(t *testing.T) {
	t.Parallel()

	work, branch, oldSHA, newSHA := divergedCheckout(t)
	target := get.Target{Owner: "kyleking", Repo: "second-look", Number: 1, Work: work, Store: work}
	seedRoundAt(t, target.Store, target.Number, oldSHA)

	pr := &forge.PullRequest{Number: 1, HeadRef: branch, HeadSHA: newSHA, State: forge.PRStatusOpen}

	if err := get.MoveWorkingCopy(t.Context(), &strings.Builder{}, target, pr); err != nil {
		t.Fatalf("checkout() = %v, want nil", err)
	}

	if got := gitIn(t, work, "rev-parse", "HEAD"); got != newSHA {
		t.Errorf("HEAD = %s, want %s", got, newSHA)
	}
}

// A checkout diverged at a commit second-look never recorded staging is not
// second-look's to discard, so it refuses rather than guessing whose work it
// is.
func TestCheckoutRefusesADivergedUpstreamItNeverStaged(t *testing.T) {
	t.Parallel()

	work, branch, oldSHA, newSHA := divergedCheckout(t)
	target := get.Target{Owner: "kyleking", Repo: "second-look", Number: 1, Work: work, Store: work}

	pr := &forge.PullRequest{Number: 1, HeadRef: branch, HeadSHA: newSHA, State: forge.PRStatusOpen}

	err := get.MoveWorkingCopy(t.Context(), &strings.Builder{}, target, pr)
	if !errors.Is(err, get.ErrDivergedUnknownWork) {
		t.Fatalf("checkout() = %v, want ErrDivergedUnknownWork", err)
	}

	if got := gitIn(t, work, "rev-parse", "HEAD"); got != oldSHA {
		t.Errorf("HEAD moved to %s despite the refusal, want it left at %s", got, oldSHA)
	}
}

// A checkout already at the pull request's head takes neither path: reaching
// vcs.PullFastForward at all here would mean fetching for no reason.
func TestCheckoutNoOpsAlreadyOnHead(t *testing.T) {
	t.Parallel()

	work, branch, oldSHA, _ := divergedCheckout(t)
	target := get.Target{Owner: "kyleking", Repo: "second-look", Number: 1, Work: work, Store: work}

	pr := &forge.PullRequest{Number: 1, HeadRef: branch, HeadSHA: oldSHA, State: forge.PRStatusOpen}

	if err := get.MoveWorkingCopy(t.Context(), &strings.Builder{}, target, pr); err != nil {
		t.Fatalf("checkout() = %v, want nil", err)
	}
}
