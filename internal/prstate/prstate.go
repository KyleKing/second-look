// Package prstate answers what the forge thinks of a pull request now: whether
// it merged, what the review decision is, and whether you have already
// reviewed it.
//
// A staged review is local work, and the one thing the local file cannot say is
// whether the work is still wanted. A pull request that merged while the review
// sat here is the case worth seeing before writing another comment on it.
package prstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNotARepository reports a name that is not owner/repo, which is the one
// input here that cannot be a forge answer.
var ErrNotARepository = errors.New("not an owner/repo name")

// State is what one pull request is now.
type State struct {
	// State is OPEN, CLOSED, or MERGED.
	State string
	// Decision is the forge's own summary: APPROVED, CHANGES_REQUESTED,
	// REVIEW_REQUIRED, or empty where the repository asks for no review.
	Decision string
	// Mine is the state of your own last review, empty where you have left
	// none. It is what stops a second sitting approving what you approved.
	Mine string
}

// Merged reports the one state that makes a staged review moot.
func (s State) Merged() bool { return s.State == "MERGED" }

// Word is the state in a few characters, and empty where there is nothing worth
// saying: an open pull request nobody has reviewed is what the queue is full of.
func (s State) Word() string {
	switch {
	case s.Merged():
		return "merged"
	case s.State == "CLOSED":
		return "closed"
	case s.Mine == "APPROVED":
		return "you approved it"
	case s.Mine == "CHANGES_REQUESTED":
		return "you asked for changes"
	case s.Mine == "COMMENTED":
		return "you commented"
	case s.Decision == "APPROVED":
		return "approved"
	case s.Decision == "CHANGES_REQUESTED":
		return "changes requested"
	}

	return ""
}

const query = `query($owner:String!,$repo:String!,$pr:Int!){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$pr){
      state
      reviewDecision
      viewerLatestReview{state}
    }
  }
}`

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type response struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				State              string `json:"state"`
				ReviewDecision     string `json:"reviewDecision"`
				ViewerLatestReview *struct {
					State string `json:"state"`
				} `json:"viewerLatestReview"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// Fetch reads one pull request's state. It is one round trip and asks for the
// three fields, because a screen that draws a row wants all three or none.
//
// The number is passed as $pr rather than $number so a recording can tell this
// call from the threads query, which takes the same owner and repo.
func Fetch(ctx context.Context, root, repo string, number int) (State, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return State{}, fmt.Errorf("%q: %w", repo, ErrNotARepository)
	}

	//nolint:gosec // every argument is a constant or a value read off the pull request
	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-F", "owner="+owner, "-F", "repo="+name, "-F", "pr="+strconv.Itoa(number),
		"-f", "query="+query)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return State{}, fmt.Errorf("reading the state of %s#%d: %w", repo, number, err)
	}

	return Decode(out)
}

// Decode reads what the query answered. It is exported because a test seeds the
// same shape the fetcher reads.
func Decode(body []byte) (State, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return State{}, fmt.Errorf("reading the pull request's state: %w", err)
	}

	pr := r.Data.Repository.PullRequest
	out := State{State: pr.State, Decision: pr.ReviewDecision}

	if pr.ViewerLatestReview != nil {
		out.Mine = pr.ViewerLatestReview.State
	}

	return out, nil
}
