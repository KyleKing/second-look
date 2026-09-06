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
				Title  string `json:"title"`
				Body   string `json:"body"`
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
				Comments struct {
					Nodes []comment `json:"nodes"`
				} `json:"comments"`
				ReviewThreads struct {
					Nodes []node `json:"nodes"`
				} `json:"reviewThreads"`
				Additions int `json:"additions"`
				Deletions int `json:"deletions"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type comment struct {
	NodeID     string `json:"id"`
	DatabaseID int64  `json:"databaseId"`
	Body       string `json:"body"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
	ReactionGroups []struct {
		Content          string `json:"content"`
		ViewerHasReacted bool   `json:"viewerHasReacted"`
		Reactors         struct {
			TotalCount int `json:"totalCount"`
		} `json:"reactors"`
	} `json:"reactionGroups"`
}

// note is the comment as this package answers it, with the reactions nobody
// left dropped: GitHub answers a group for every emoji whether or not anyone
// used it.
func (c *comment) note() Note {
	out := Note{ID: c.DatabaseID, NodeID: c.NodeID, Author: c.Author.Login, Body: c.Body}

	for _, g := range c.ReactionGroups {
		if g.Reactors.TotalCount == 0 {
			continue
		}

		out.Reactions = append(out.Reactions, Reaction{
			Content: g.Content, Count: g.Reactors.TotalCount, Mine: g.ViewerHasReacted,
		})
	}

	return out
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type node struct {
	IsResolved   bool   `json:"isResolved"`
	IsOutdated   bool   `json:"isOutdated"`
	Path         string `json:"path"`
	Line         int    `json:"line"`
	OriginalLine int    `json:"originalLine"`
	DiffSide     string `json:"diffSide"`
	Comments     struct {
		Nodes []comment `json:"nodes"`
	} `json:"comments"`
}

// Decode reads what the GraphQL query answered. It is exported because a test
// that seeds a cached thread reads the same recording the fetcher does, and two
// copies of GitHub's shape would drift.
func Decode(body []byte) ([]Thread, About, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, About{}, fmt.Errorf("reading the review threads: %w", err)
	}

	return r.threads(), r.about(), nil
}

func (r *response) about() About {
	pr := &r.Data.Repository.PullRequest

	out := About{
		Title: pr.Title, Author: pr.Author.Login, Body: pr.Body,
		Added: pr.Additions, Removed: pr.Deletions,
	}

	for _, l := range pr.Labels.Nodes {
		out.Labels = append(out.Labels, l.Name)
	}

	for _, c := range pr.Comments.Nodes {
		out.Comments = append(out.Comments, c.note())
	}

	return out
}

func (r *response) threads() []Thread {
	nodes := r.Data.Repository.PullRequest.ReviewThreads.Nodes
	out := make([]Thread, 0, len(nodes))

	for i := range nodes {
		n := &nodes[i]

		// GitHub nulls line for an outdated thread; originalLine is where it
		// stayed anchored, and is what lets one still be shown at all.
		line := n.Line
		if line == 0 {
			line = n.OriginalLine
		}

		if line == 0 || len(n.Comments.Nodes) == 0 {
			continue
		}

		t := Thread{Path: n.Path, Side: n.DiffSide, Line: line, Resolved: n.IsResolved, Outdated: n.IsOutdated}
		for _, c := range n.Comments.Nodes {
			t.Notes = append(t.Notes, c.note())
		}

		out = append(out, t)
	}

	return out
}
