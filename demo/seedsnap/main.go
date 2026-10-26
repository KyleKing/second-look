// Command seedsnap writes the inbox snapshot a scene opens against: the
// fixture's searches with each bucket reversed, one row missing, and one the
// forge no longer returns, so the refresh has something to move.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/snapshot"
)

// searches is the fixture file each default bucket's search is answered from.
var searches = []string{"search-pending.json", "search-open.json", "search-authored.json", "search-merged.json"}

// pageSize is the limit the default plan's searches ask for, goneNumber is the
// pull request the snapshot holds and the forge does not, and savedAgo is how
// old the snapshot reads.
const (
	pageSize   = 30
	goneNumber = 7
	usageExit  = 2
	savedAgo   = 3 * time.Minute
)

func main() {
	if len(os.Args) != 2 { //nolint:mnd // one argument: the fixture directory
		fmt.Fprintln(os.Stderr, "usage: seedsnap <fixture dir>")
		os.Exit(usageExit)
	}

	if err := seed(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "seedsnap:", err)
		os.Exit(1)
	}
}

func seed(fixture string) error {
	base, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("finding the cache directory: %w", err)
	}

	buckets := make([]snapshot.Bucket, 0, len(searches))
	plan := inbox.Plan(pageSize)

	for i, file := range searches {
		items, err := read(filepath.Join(fixture, file))
		if err != nil {
			return err
		}

		slices.Reverse(items)

		if i == 0 && len(items) > 1 {
			items = items[1:]
		}

		if i == 1 {
			items = append(items, inbox.PullRequest{
				ID: "PR_kwDO0999", Repository: "example/gone", Number: goneNumber, Author: "octocat",
				Title: "fix: a row the forge no longer returns", Created: items[0].Created, Updated: items[0].Updated,
			})
		}

		buckets = append(buckets, snapshot.Bucket{Name: plan[i].Name, Items: snapshot.RowsOf(items)})
	}

	saved := time.Now().Add(-savedAgo)
	snapshot.Open(cache.NewDiskCache(filepath.Join(base, "second-look")), func() time.Time { return saved }).
		SaveInbox("kyleking", snapshot.Inbox{Buckets: buckets, Facts: map[string]inbox.Record{}})

	return nil
}

func read(path string) ([]inbox.PullRequest, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is a fixture file named on the command line
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	//nolint:tagliatelle // gh answers in camelCase and these names are its own
	var got []struct {
		ID         string `json:"id"`
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		IsDraft bool   `json:"isDraft"`
		Author  struct {
			Login string `json:"login"`
		} `json:"author"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
		URL       string    `json:"url"`
		Labels    []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}

	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	out := make([]inbox.PullRequest, 0, len(got))

	for i := range got {
		g := &got[i]
		p := inbox.PullRequest{
			ID: g.ID, Repository: g.Repository.NameWithOwner, Number: g.Number, Title: g.Title,
			Author: g.Author.Login, Draft: g.IsDraft, Created: g.CreatedAt, Updated: g.UpdatedAt, URL: g.URL,
		}

		for _, l := range g.Labels {
			p.Labels = append(p.Labels, l.Name)
		}

		out = append(out, p)
	}

	return out, nil
}
