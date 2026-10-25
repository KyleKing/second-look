// Package people finds the logins an @ can name: whoever is on a pull request,
// and whoever has been on the repository's recent ones.
//
// GitHub's own answer for a bot is its login without the [bot] suffix the REST
// API adds, which is what a mention has to spell.
package people

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/ghrun"
)

const ttl = 24 * time.Hour

var mem = cache.NewRegistered[[]string](ttl) //nolint:gochecknoglobals // the process-wide roster memory

type who struct {
	Login string `json:"login"`
}

type authored struct {
	Author who `json:"author"`
}

type nodes[T any] struct {
	Nodes []T `json:"nodes"`
}

//nolint:tagliatelle // GitHub's GraphQL names are camelCase and its own
type recentAnswer struct {
	Data struct {
		Repository struct {
			PullRequests nodes[struct {
				authored
				Comments nodes[authored] `json:"comments"`
				Reviews  nodes[authored] `json:"reviews"`
			}] `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

//nolint:tagliatelle // GitHub's GraphQL names are camelCase and its own
type involvedAnswer struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				authored
				Participants   nodes[who] `json:"participants"`
				ReviewRequests nodes[struct {
					Reviewer who `json:"requestedReviewer"`
				}] `json:"reviewRequests"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

const recentQuery = `query($owner:String!,$name:String!){
  repository(owner:$owner,name:$name){
    pullRequests(first:30,orderBy:{field:UPDATED_AT,direction:DESC}){nodes{
      author{login}
      comments(last:20){nodes{author{login}}}
      reviews(last:20){nodes{author{login}}}
    }}
  }
}`

const involvedQuery = `query($owner:String!,$name:String!,$number:Int!){
  repository(owner:$owner,name:$name){
    pullRequest(number:$number){
      author{login}
      participants(first:100){nodes{login}}
      reviewRequests(first:30){nodes{requestedReviewer{... on User{login} ... on Bot{login}}}}
    }
  }
}`

func args(query, repo string, extra ...string) []string {
	owner, name, _ := strings.Cut(repo, "/")

	return append([]string{
		"api", "graphql", "-F", "owner=" + owner, "-F", "name=" + name, "-f", "query=" + query,
	}, extra...)
}

// RecentArgs is the gh call that reads who commented on and reviewed the
// repository's recent pull requests.
func RecentArgs(repo string) []string { return args(recentQuery, repo) }

// InvolvedArgs is the gh call that reads who is on one pull request.
func InvolvedArgs(repo string, number int) []string {
	return args(involvedQuery, repo, "-F", fmt.Sprintf("number=%d", number))
}

// Recent is the logins that wrote or reviewed the repository's last thirty
// updated pull requests, one gh call per repository per day.
func Recent(ctx context.Context, root, repo string) ([]string, error) {
	if got, ok := cache.Persisted(mem, "people", repo, cache.NoStamp); ok {
		return got, nil
	}

	var answer recentAnswer

	if err := read(ctx, root, RecentArgs(repo), &answer); err != nil {
		return nil, fmt.Errorf("reading who is active on %s: %w", repo, err)
	}

	var logins []string

	for _, pr := range answer.Data.Repository.PullRequests.Nodes {
		logins = append(logins, pr.Author.Login)

		for _, c := range slices.Concat(pr.Comments.Nodes, pr.Reviews.Nodes) {
			logins = append(logins, c.Author.Login)
		}
	}

	logins = tidy(logins)
	cache.Persist(mem, "people", repo, cache.NoStamp, logins)

	return logins, nil
}

// Involved is the pull request's author, its participants, and who it asks to
// review it, which is who a comment on it is likely to name.
func Involved(ctx context.Context, root, repo string, number int) ([]string, error) {
	var answer involvedAnswer

	if err := read(ctx, root, InvolvedArgs(repo, number), &answer); err != nil {
		return nil, fmt.Errorf("reading who is on %s#%d: %w", repo, number, err)
	}

	pr := answer.Data.Repository.PullRequest
	logins := []string{pr.Author.Login}

	for _, p := range pr.Participants.Nodes {
		logins = append(logins, p.Login)
	}

	for _, r := range pr.ReviewRequests.Nodes {
		logins = append(logins, r.Reviewer.Login)
	}

	return tidy(logins), nil
}

func read(ctx context.Context, root string, args []string, into any) error {
	out, err := ghrun.Output(ctx, root, args...)
	if err != nil {
		return fmt.Errorf("asking GitHub: %w", err)
	}

	if err := json.Unmarshal(out, into); err != nil {
		return fmt.Errorf("decoding the answer: %w", err)
	}

	return nil
}

func tidy(logins []string) []string {
	logins = slices.DeleteFunc(logins, func(l string) bool { return l == "" })
	slices.Sort(logins)

	return slices.Compact(logins)
}
