// Package checkouts answers which local clones hold a repository.
//
// One remote is often cloned several times on one laptop, plus its worktrees,
// and knowing which directory holds which branch is gh-repo-dashboard's job
// rather than this tool's. So this asks it, through `gh repo-dashboard --cli`,
// which reads its own cache and touches no network.
//
// Nothing here moves a checkout. It reports the candidates and the order to try
// them in, and the caller does the moving.
package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// ErrNoDashboard reports the query itself failing, which on a laptop that has
// never installed the extension is the whole answer.
var ErrNoDashboard = errors.New("gh repo-dashboard could not be run; " +
	"install it with gh extension install kyleking/gh-repo-dashboard")

// ErrNoRemotes reports an answer carrying no remote identity at all, which is
// what an older gh-repo-dashboard returns: it printed no remote field, so no
// directory in it can be matched to a pull request.
var ErrNoRemotes = errors.New("gh repo-dashboard reported no remotes; upgrade it")

// Checkout is one local clone or worktree that holds the repository.
type Checkout struct {
	Path   string
	Branch string
	Dirty  bool
	// Worktree marks a path that came from another checkout's worktree list
	// rather than from a scan, which is worth saying when a reader is asked to
	// choose.
	Worktree bool
}

// Runner runs the dashboard and returns what it printed. The extension is the
// only implementation that ships; a test supplies its own.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type ghRunner struct{}

// Dashboard queries gh-repo-dashboard through the gh extension.
//
//nolint:ireturn // Runner is the seam a test replaces; concrete would remove it
func Dashboard() Runner { return ghRunner{} }

func (ghRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	//nolint:gosec // every argument is a constant or a repository name
	cmd := exec.CommandContext(ctx, "gh", append([]string{"repo-dashboard"}, args...)...)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh repo-dashboard --cli: %w", err)
	}

	return out, nil
}

// fleet is the part of the dashboard's JSON this reads. Everything else it
// prints is about repository health rather than about where a branch lives.
type fleet struct {
	Repos []repo `json:"repos"`
}

type repo struct {
	Path      string     `json:"path"`
	Branch    string     `json:"branch"`
	Remote    string     `json:"remote"`
	RemoteID  string     `json:"remote_id"`
	Worktrees []worktree `json:"worktrees"`
	Dirty     bool       `json:"dirty"`
}

type worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// Depth is how deep the scan goes when the dashboard has no configured scan
// paths of its own. Two levels covers an owner directory holding repositories.
const Depth = "2"

// Find is every local checkout of repo, best first.
//
// The dashboard is asked with no paths, so it reads the scan paths from its own
// config and answers for the whole fleet from cache. The head argument is the
// branch the pull request is on, which decides the ranking and may be empty.
func Find(ctx context.Context, r Runner, repo, head string) ([]Checkout, error) {
	raw, err := r.Run(ctx, "--cli", "-depth", Depth)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoDashboard, err)
	}

	var got fleet
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, fmt.Errorf("reading what gh repo-dashboard printed: %w", err)
	}

	named := false
	seen := map[string]bool{}

	var out []Checkout

	for i := range got.Repos {
		one := &got.Repos[i]
		if one.Remote != "" || one.RemoteID != "" {
			named = true
		}

		if !holds(one.Remote, one.RemoteID, repo) {
			continue
		}

		for _, c := range spread(one) {
			if seen[c.Path] {
				continue
			}

			seen[c.Path] = true
			out = append(out, c)
		}
	}

	if !named && len(got.Repos) > 0 {
		return nil, ErrNoRemotes
	}

	rank(out, head)

	return out, nil
}

// spread is the checkout plus its worktrees, since a worktree is a directory a
// review can happen in and the branch it holds is the reason to prefer it.
func spread(one *repo) []Checkout {
	out := []Checkout{{Path: one.Path, Branch: one.Branch, Dirty: one.Dirty}}

	for _, w := range one.Worktrees {
		if w.Path == one.Path {
			continue
		}

		out = append(out, Checkout{Path: w.Path, Branch: w.Branch, Worktree: true})
	}

	return out
}

// holds reports whether a scanned directory belongs to repo. The comparison
// folds case, because GitHub answers the owner's chosen capitalization and a
// remote URL carries whatever was typed when it was cloned.
func holds(remote, remoteID, repo string) bool {
	if repo == "" {
		return false
	}

	if strings.EqualFold(remote, repo) {
		return true
	}

	return remoteID != "" && strings.HasSuffix(strings.ToLower(remoteID), "/"+strings.ToLower(repo))
}

// rank orders the candidates by how little the reviewer has to give up to use
// one: already on the branch costs nothing, a clean tree costs a branch switch,
// and a dirty tree costs the stash question.
func rank(out []Checkout, head string) {
	// What each candidate costs to use: nothing, a branch switch, or the stash
	// question.
	const (
		standingOnIt = iota
		aSwitch
		aStash
	)

	score := func(c Checkout) int {
		switch {
		case head != "" && strings.EqualFold(c.Branch, head):
			return standingOnIt
		case !c.Dirty:
			return aSwitch
		default:
			return aStash
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if a, b := score(out[i]), score(out[j]); a != b {
			return a < b
		}

		return out[i].Path < out[j].Path
	})
}
