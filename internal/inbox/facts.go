package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// Event is something a human did to a pull request, and when.
type Event struct {
	Author string    `json:"author"`
	State  string    `json:"state,omitempty"`
	At     time.Time `json:"at"`
}

// Facts is what a search cannot say about a pull request. Every person in it is
// a human: bot reviews and team requests are dropped on the way in.
type Facts struct {
	Decision  string    `json:"decision,omitempty"`
	Checks    string    `json:"checks,omitempty"`
	Requested []string  `json:"requested,omitempty"`
	Reviews   []Event   `json:"reviews,omitempty"`
	Talk      []Event   `json:"talk,omitempty"`
	Pushed    time.Time `json:"pushed,omitzero"`
	Dirs      []string  `json:"dirs,omitempty"`
	// Base and Head are the branches the pull request joins, which is how a
	// stack is told from rows that merely share a repository.
	Base string `json:"base,omitempty"`
	Head string `json:"head,omitempty"`
	// Oid is the head commit, kept so a snapshot says which push it was read at.
	Oid string `json:"oid,omitempty"`
}

// Fetched is one read of Facts keyed like a queue row, with the login it was read as.
type Fetched struct {
	Viewer string
	Facts  map[string]Facts
}

// maxNodes is what GitHub lets one nodes() lookup ask for.
const maxNodes = 100

const query = `query($ids: [ID!]!) {
  viewer { login }
  nodes(ids: $ids) {
    ... on PullRequest {
      number
      repository { nameWithOwner }
      baseRefName
      headRefName
      headRefOid
      reviewDecision
      reviewRequests(first: 20) {
        nodes { requestedReviewer { __typename ... on User { login } } }
      }
      latestReviews(first: 20) {
        nodes { author { __typename login } state submittedAt }
      }
      comments(last: 20) { nodes { author { __typename login } createdAt } }
      commits(last: 1) {
        nodes { commit { committedDate statusCheckRollup { state } } }
      }
      files(first: 100) { nodes { path } }
    }
  }
}`

// FactsArgs is the gh call one read of up to maxNodes pull requests makes, exported
// so a test replaying it builds its interaction from this rather than a copy.
func FactsArgs(ids []string) []string {
	args := make([]string, 0, 4+2*len(ids))
	args = append(args, "api", "graphql", "-f", "query="+query)
	for _, id := range ids {
		args = append(args, "-f", "ids[]="+id)
	}

	return args
}

// Fetch reads the facts for every row in one request, or one per hundred rows
// where a configured queue is longer. A row GitHub does not answer for is absent.
func Fetch(ctx context.Context, root string, rows []PullRequest) (Fetched, error) {
	out := Fetched{Facts: map[string]Facts{}}

	chunks := slices.Collect(slices.Chunk(nodeIDs(rows), maxNodes))
	parts := make([]Fetched, len(chunks))
	errs := make([]error, len(chunks))

	var wait sync.WaitGroup

	for i := range chunks {
		wait.Add(1)

		go func() {
			defer wait.Done()

			parts[i], errs[i] = fetchChunk(ctx, root, chunks[i])
		}()
	}

	wait.Wait()

	for i := range parts {
		if parts[i].Viewer != "" {
			out.Viewer = parts[i].Viewer
		}

		maps.Copy(out.Facts, parts[i].Facts)
	}

	return out, errors.Join(errs...)
}

func nodeIDs(rows []PullRequest) []string {
	ids := make([]string, 0, len(rows))

	for i := range rows {
		if id := rows[i].ID; id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}

	return ids
}

func fetchChunk(ctx context.Context, root string, ids []string) (Fetched, error) {
	//nolint:gosec // the query is a constant and the ids are node ids GitHub returned
	cmd := exec.CommandContext(ctx, "gh", FactsArgs(ids)...)
	cmd.Dir = root

	body, err := cmd.Output()
	if err != nil {
		return Fetched{}, fmt.Errorf("reading reviews: %w", ghError(err))
	}

	return DecodeFacts(body)
}

//nolint:tagliatelle // __typename is GraphQL's own name
type person struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

func (p *person) human() bool {
	return p != nil && p.Typename == "User" && p.Login != "" && !strings.HasSuffix(p.Login, "[bot]")
}

//nolint:tagliatelle // GitHub answers in camelCase and these names are its own
type node struct {
	Number     int `json:"number"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	BaseRefName    string `json:"baseRefName"`
	HeadRefName    string `json:"headRefName"`
	HeadRefOid     string `json:"headRefOid"`
	ReviewDecision string `json:"reviewDecision"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *person `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	LatestReviews struct {
		Nodes []struct {
			Author      *person   `json:"author"`
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submittedAt"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	Comments struct {
		Nodes []struct {
			Author    *person   `json:"author"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"nodes"`
	} `json:"comments"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				CommittedDate     time.Time `json:"committedDate"`
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Files struct {
		Nodes []struct {
			Path string `json:"path"`
		} `json:"nodes"`
	} `json:"files"`
}

// DecodeFacts reads gh's answer to the query.
func DecodeFacts(body []byte) (Fetched, error) {
	var reply struct {
		Data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
			Nodes []*node `json:"nodes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &reply); err != nil {
		return Fetched{}, fmt.Errorf("reading the reviews: %w", err)
	}

	out := Fetched{Viewer: reply.Data.Viewer.Login, Facts: map[string]Facts{}}

	for _, n := range reply.Data.Nodes {
		if n == nil || n.Repository.NameWithOwner == "" {
			continue
		}

		out.Facts[fmt.Sprintf("%s#%d", n.Repository.NameWithOwner, n.Number)] = n.facts()
	}

	return out, nil
}

func (n *node) facts() Facts {
	f := Facts{Decision: n.ReviewDecision, Base: n.BaseRefName, Head: n.HeadRefName, Oid: n.HeadRefOid}

	for _, r := range n.ReviewRequests.Nodes {
		if r.RequestedReviewer.human() {
			f.Requested = append(f.Requested, r.RequestedReviewer.Login)
		}
	}

	for _, r := range n.LatestReviews.Nodes {
		if r.Author.human() {
			f.Reviews = append(f.Reviews, Event{Author: r.Author.Login, State: r.State, At: r.SubmittedAt})
		}
	}

	for _, c := range n.Comments.Nodes {
		if c.Author.human() {
			f.Talk = append(f.Talk, Event{Author: c.Author.Login, At: c.CreatedAt})
		}
	}

	if len(n.Commits.Nodes) > 0 {
		last := n.Commits.Nodes[0].Commit
		f.Pushed = last.CommittedDate

		if last.StatusCheckRollup != nil {
			f.Checks = last.StatusCheckRollup.State
		}
	}

	paths := make([]string, 0, len(n.Files.Nodes))
	for _, file := range n.Files.Nodes {
		paths = append(paths, file.Path)
	}

	f.Dirs = TopDirs(paths)

	return f
}

// TopDirs is the sorted, distinct first segment of every path that sits in a
// directory. A file at the root names no area.
func TopDirs(paths []string) []string {
	seen := map[string]bool{}

	for _, p := range paths {
		if dir, _, nested := strings.Cut(p, "/"); nested {
			seen[dir] = true
		}
	}

	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}

	slices.Sort(dirs)

	return dirs
}
