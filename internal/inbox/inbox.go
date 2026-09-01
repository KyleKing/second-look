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

	"github.com/kyleking/aragonite/forge/github"
)

// Section is one configured query, under the name it is shown by.
type Section struct {
	Name  string
	Query string
}

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
	return run(ctx, root, []Bucket{
		{Name: "pending your review", query: built(limit, "--review-requested=@me", "--state=open")},
		{Name: "reviewed, still open", query: built(limit, "--reviewed-by=@me", "--state=open")},
		{Name: "reviewed, merged", query: built(limit, "--reviewed-by=@me", "--merged")},
	})
}

// Configured is the queue a config asked for: one bucket per section, in the
// order the file names them, each a gh search query of its own.
//
// A query carrying a sort: qualifier is turned into gh's own flags, and one
// naming no subject is scoped to what involves you, both by aragonite, so a
// query written for gh-dash or pasted out of GitHub's search box answers the
// same way here.
func Configured(ctx context.Context, root string, sections []Section, limit int) []Bucket {
	out := make([]Bucket, 0, len(sections))

	for i := range sections {
		out = append(out, Bucket{
			Name:  sections[i].Name,
			query: github.FleetSearchArgs(sections[i].Query, page(limit)...),
		})
	}

	return run(ctx, root, out)
}

// built is a built-in bucket's whole argument list. Recency is its order,
// because a queue is read from the top and the stalest row is rarely the next
// one to do.
func built(limit int, filters ...string) []string {
	return append(append(filters, "--sort", "updated"), page(limit)...)
}

// page is what every search asks for however it was written.
func page(limit int) []string {
	return []string{"--limit", strconv.Itoa(limit), "--json", fields}
}

func run(ctx context.Context, root string, out []Bucket) []Bucket {
	for i := range out {
		items, err := search(ctx, root, out[i].query...)
		if err != nil {
			out[i].Err = err.Error()

			continue
		}

		out[i].Items = items
	}

	return out
}

func search(ctx context.Context, root string, tail ...string) ([]PullRequest, error) {
	args := append([]string{"search", "prs"}, tail...)

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
