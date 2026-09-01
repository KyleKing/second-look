package threads

import (
	"encoding/json"
	"fmt"
)

// response is the GraphQL reply, shaped only where this package reads it.
//

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type response struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []node `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type node struct {
	IsResolved bool   `json:"isResolved"`
	IsOutdated bool   `json:"isOutdated"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	DiffSide   string `json:"diffSide"`
	Comments   struct {
		Nodes []struct {
			DatabaseID int64  `json:"databaseId"`
			Body       string `json:"body"`
			Author     struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"nodes"`
	} `json:"comments"`
}

// Decode reads what the GraphQL query answered. It is exported because a test
// that seeds a cached thread reads the same recording the fetcher does, and two
// copies of GitHub's shape would drift.
func Decode(body []byte) ([]Thread, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("reading the review threads: %w", err)
	}

	return r.threads(), nil
}

func (r *response) threads() []Thread {
	nodes := r.Data.Repository.PullRequest.ReviewThreads.Nodes
	out := make([]Thread, 0, len(nodes))

	for i := range nodes {
		n := &nodes[i]
		if n.IsResolved || n.IsOutdated || n.Line == 0 || len(n.Comments.Nodes) == 0 {
			continue
		}

		t := Thread{Path: n.Path, Side: n.DiffSide, Line: n.Line}
		for _, c := range n.Comments.Nodes {
			t.Notes = append(t.Notes, Note{ID: c.DatabaseID, Author: c.Author.Login, Body: c.Body})
		}

		out = append(out, t)
	}

	return out
}
