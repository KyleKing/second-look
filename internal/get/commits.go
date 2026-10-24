package get

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/commits"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/plan"
)

// Commits is the pull request's commits with the hunks of d each wrote, blamed at head
// in the checkout. Where the checkout cannot answer, the reason says why instead.
func Commits(ctx context.Context, t Target, head string, d *diff.Diff) ([]plan.Commit, string, error) {
	if t.Work == "" {
		return nil, "no checkout holds the pull request's commits", nil
	}

	listed, err := listCommits(ctx, t)
	if err != nil {
		return nil, "", err
	}

	if len(listed) == 0 {
		return nil, "the pull request lists no commits", nil
	}

	missing, err := absent(ctx, t.Work, listed)
	if err != nil {
		return nil, "", err
	}

	if missing > 0 {
		return nil, fmt.Sprintf("%d of the pull request's %d commits are not in this checkout",
			missing, len(listed)), nil
	}

	mapped, err := commits.Blame(ctx, t.Work, head, listed, d)
	if err != nil {
		return nil, "", fmt.Errorf("mapping hunks onto #%d's commits: %w", t.Number, err)
	}

	return mapped, "", nil
}

func listCommits(ctx context.Context, t Target) ([]plan.Commit, error) {
	args := []string{"pr", "view", strconv.Itoa(t.Number), "--json", "commits"}
	if t.Remote() != "" {
		args = append(args, "--repo", t.Remote())
	}

	cmd := exec.CommandContext(ctx, "gh", args...) // #nosec G204 -- constants and the review's own number and name
	cmd.Dir = t.Dir()

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing #%d's commits: %w", t.Number, ghReason(err))
	}

	//nolint:tagliatelle // gh answers in camelCase and these names are its own
	var answer struct {
		Commits []struct {
			OID      string `json:"oid"`
			Headline string `json:"messageHeadline"`
		} `json:"commits"`
	}

	if err := json.Unmarshal(out, &answer); err != nil {
		return nil, fmt.Errorf("reading #%d's commits: %w", t.Number, err)
	}

	list := make([]plan.Commit, 0, len(answer.Commits))
	for _, c := range answer.Commits {
		list = append(list, plan.Commit{ID: c.OID, Subject: c.Headline})
	}

	return list, nil
}

// absent is how many of the commits the checkout does not hold.
func absent(ctx context.Context, root string, list []plan.Commit) (int, error) {
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID+"^{commit}")
	}

	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch-check")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")

	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("checking which commits this checkout holds: %w", err)
	}

	return strings.Count(string(out), " missing\n"), nil
}
