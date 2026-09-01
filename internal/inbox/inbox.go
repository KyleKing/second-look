// Package inbox is the review queue: what is waiting on you, what you have
// already answered and is still open, and what has since merged.
//
// The buckets are the whole point. A flat list of pull requests says nothing
// about what to do next, and the three states a reviewer actually distinguishes
// are "nobody has looked", "I looked and it is still moving", and "it landed".
package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Bucket is one section of the queue, in the order they want doing.
type Bucket struct {
	Name  string        `json:"bucket"`
	Items []PullRequest `json:"items"`
	// Err is why this bucket is empty, when it is empty for a reason. One
	// search failing takes its own bucket down and not the queue: a rate limit
	// on the merged list is no reason to stop showing what is waiting.
	Err string `json:"error,omitempty"`

	query []string
}

// PullRequest is what triage needs without opening the pull request.
type PullRequest struct {
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Author     string    `json:"author"`
	Draft      bool      `json:"draft"`
	Comments   int       `json:"comments"`
	Labels     []string  `json:"labels"`
	Updated    time.Time `json:"updated"`
	URL        string    `json:"url"`
}

// fields is what gh search answers with, kept to what a triage line shows.
const fields = "repository,number,title,author,isDraft,commentsCount,labels,updatedAt,url"

// Buckets is the queue, in order: what is waiting on you first, because that is
// the only bucket with anything to do in it.
func Buckets(ctx context.Context, root string, limit int) []Bucket {
	out := []Bucket{
		{Name: "pending your review", query: []string{"--review-requested=@me", "--state=open"}},
		{Name: "reviewed, still open", query: []string{"--reviewed-by=@me", "--state=open"}},
		{Name: "reviewed, merged", query: []string{"--reviewed-by=@me", "--merged"}},
	}

	for i := range out {
		items, err := search(ctx, root, limit, out[i].query...)
		if err != nil {
			out[i].Err = err.Error()

			continue
		}

		out[i].Items = items
	}

	return out
}

func search(ctx context.Context, root string, limit int, filters ...string) ([]PullRequest, error) {
	args := append([]string{"search", "prs"}, filters...)
	args = append(args, "--sort", "updated", "--limit", strconv.Itoa(limit), "--json", fields)

	//nolint:gosec // every argument is a constant or an integer from the caller
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, ghError(err)
	}

	var raw []result
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("reading the search results: %w", err)
	}

	prs := make([]PullRequest, 0, len(raw))
	for i := range raw {
		prs = append(prs, raw[i].pullRequest())
	}

	return prs, nil
}

// ghError puts gh's own stderr in the message, which is where its reason for
// refusing lives; the exit status alone says nothing.
func ghError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("gh search: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}

	return fmt.Errorf("gh search: %w", err)
}
