// Package labels reads a repository's labels and sets them on a pull request.
package labels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/ghrun"
)

// Label is one label a repository defines.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Target is a pull request, with Repo as owner/name.
type Target struct {
	Repo   string
	Number int
}

const ttl = 24 * time.Hour

var mem = cache.NewRegistered[[]Label](ttl) //nolint:gochecknoglobals // the process-wide label memory

// ListArgs is the gh call that lists a repository's labels, past gh's page of thirty.
func ListArgs(repo string) []string {
	return []string{"label", "list", "--repo", repo, "--json", "name,color,description", "--limit", "1000"}
}

// Read lists the labels of repo, one gh call per repository per day.
func Read(ctx context.Context, root, repo string) ([]Label, error) {
	if got, ok := cache.Persisted(mem, "labels", repo, cache.NoStamp); ok {
		return got, nil
	}

	out, err := ghrun.Output(ctx, root, ListArgs(repo)...)
	if err != nil {
		return nil, fmt.Errorf("listing the labels of %s: %w", repo, err)
	}

	var all []Label
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("reading the labels of %s: %w", repo, err)
	}

	cache.Persist(mem, "labels", repo, cache.NoStamp, all)

	return all, nil
}

// SetArgs is the gh call that puts a label on a pull request or takes it off.
func SetArgs(at Target, name string, on bool) []string {
	path := fmt.Sprintf("/repos/%s/issues/%d/labels", at.Repo, at.Number)
	if on {
		return []string{"api", "--method", "POST", path, "-f", "labels[]=" + name}
	}

	return []string{"api", "--method", "DELETE", path + "/" + url.PathEscape(name)}
}

// Set adds the label to the pull request, or removes it.
func Set(ctx context.Context, run ghrun.Runner, root string, at Target, name string, on bool) error {
	verb := "adding"
	if !on {
		verb = "removing"
	}

	if err := run.Run(ctx, root, SetArgs(at, name, on)...); err != nil {
		return fmt.Errorf("%s label %s: %w", verb, name, err)
	}

	return nil
}

// With is names with name added or taken out, in the order a pull request
// carries them.
func With(names []string, name string, on bool) []string {
	out := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == name })
	if on {
		out = append(out, name)
	}

	return out
}

// Tags is how a row or a header says what a pull request is labeled.
func Tags(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "[" + n + "]"
	}

	return strings.Join(parts, " ")
}
