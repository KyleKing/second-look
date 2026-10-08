// Package versions answers what a dependency's registry can say about the
// version a lockfile moved to: when it shipped, what the registry calls
// current, and where the source lives. Answers are cached in memory and on
// disk where the process installed a store, so a second review of the same
// package asks nothing.
package versions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/advisory"
)

// Card is what a registry answers for one dependency at the version under
// review. Every field is empty where the registry does not carry it, and
// Missing is the registry answering that no such package exists.
type Card struct {
	// Latest is the version the registry calls current.
	Latest string
	// Released is when the version under review shipped.
	Released time.Time
	// Source is the repository link the registry carries. For Go modules the
	// registry carries none, so it is the forge the module path names, or the
	// module's pkg.go.dev page where the path is not a forge's.
	Source string
	// Summary is the registry's one-line description.
	Summary string
	// Missing reports that the registry knows no package by this name.
	Missing bool
}

// info is the whole of what a registry said about a package, kept rather than
// the card one version asked for, so a review of the same package at another
// version still reads it.
type info struct {
	Latest   string
	Released map[string]time.Time
	Source   string
	Summary  string
	Missing  bool
}

// Client asks the registries. The zero value asks the public endpoints through
// the default HTTP client.
type Client struct {
	HTTP *http.Client
	// Bases overrides each ecosystem's registry root, which is how a test
	// points a fetcher at its own server. An ecosystem absent from it keeps
	// the public endpoint.
	Bases map[string]string
}

// bases is each ecosystem's public registry.
var bases = map[string]string{ //nolint:gochecknoglobals // a read-only table of endpoints
	"Go":        "https://proxy.golang.org",
	"PyPI":      "https://pypi.org",
	"npm":       "https://registry.npmjs.org",
	"crates.io": "https://crates.io",
	"RubyGems":  "https://rubygems.org",
}

// errNoRegistry is an ecosystem this package has no fetcher for, and
// errRefused is the registry answering with anything but an answer or a 404.
var (
	errNoRegistry = errors.New("has no registry here")
	errRefused    = errors.New("the registry refused the ask")
)

// ttl is how long a fetched answer stands. A release's date is fixed the day
// it ships and the latest pointer moves slowly, so a day is fresh enough for
// what a review asks of them.
const ttl = 24 * time.Hour

// mem is the process's memory of answers already fetched. The disk underneath
// it is opt-in through cache.SetDiskCache.
var mem = cache.NewRegistered[info](ttl) //nolint:gochecknoglobals // the process-wide card memory

// timeout bounds the whole question. A packument is the biggest answer any
// registry gives and lands in well under this against a working network.
const timeout = 30 * time.Second

// widen is how many packages are fetched at once. One package is one small
// request, so this guards a bad day rather than tuning throughput.
const widen = 8

// Cards asks each package's registry what it knows, one fetch per package
// name, cached answers reading back for free. A package that could not be
// fetched is absent from the answer and its error is joined into the result;
// a package the registry has never heard of is present and Missing.
func (c Client) Cards(ctx context.Context, pkgs []advisory.Package) (map[advisory.Package]Card, error) {
	out := make(map[advisory.Package]Card, len(pkgs))
	if len(pkgs) == 0 {
		return out, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var mu sync.Mutex

	var errs []error

	gate := make(chan struct{}, widen)

	var wg sync.WaitGroup

	for _, p := range pkgs {
		wg.Add(1)

		gate <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-gate }()

			card, err := c.card(ctx, p)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err != nil:
				errs = append(errs, err)
			default:
				out[p] = card
			}
		}()
	}

	wg.Wait()

	return out, errors.Join(errs...)
}

// card is one package's answer, cached under its name so a review at another
// version still reads it. A cached entry missing only a version's date pays
// for that version alone.
func (c Client) card(ctx context.Context, p advisory.Package) (Card, error) {
	key := p.Ecosystem + "/" + p.Name

	i, known := cache.Persisted(mem, "versions", key, cache.NoStamp)

	fresh := known && (i.Missing || p.Version == i.Latest || hasDate(i, p.Version))
	if !fresh {
		got, err := c.fetch(ctx, p.Ecosystem, p.Name, p.Version)
		switch {
		case errors.Is(err, errNotFound):
			i = info{Missing: true}
		case err != nil:
			return Card{}, fmt.Errorf("%s %s: %w", p.Ecosystem, p.Name, err)
		default:
			i = merge(i, got)
		}

		// A version the registry does not date is recorded undated, which is
		// what keeps the next review of it from asking again.
		if i.Released == nil {
			i.Released = map[string]time.Time{}
		}
		if _, has := i.Released[p.Version]; !has {
			i.Released[p.Version] = time.Time{}
		}

		cache.Persist(mem, "versions", key, cache.NoStamp, i)
	}

	return Card{
		Latest:   i.Latest,
		Released: i.Released[p.Version],
		Source:   i.Source,
		Summary:  i.Summary,
		Missing:  i.Missing,
	}, nil
}

func hasDate(i info, version string) bool {
	_, ok := i.Released[version]

	return ok
}

// merge folds a fresh answer into the cached one, keeping release dates the
// fetch did not carry and preferring its word on everything else.
func merge(old, got info) info {
	got.Released = mergeDates(old.Released, got.Released)

	if got.Missing {
		return got
	}

	if got.Latest == "" {
		got.Latest = old.Latest
	}
	if got.Source == "" {
		got.Source = old.Source
	}
	if got.Summary == "" {
		got.Summary = old.Summary
	}

	return got
}

func mergeDates(old, got map[string]time.Time) map[string]time.Time {
	if got == nil {
		return old
	}
	for v, t := range old {
		if _, has := got[v]; !has {
			got[v] = t
		}
	}

	return got
}

// fetch reads one package's page(s) into info. Registries that answer every
// version at once fill Released for all of them, so a later review at another
// version reads the cache rather than asking again; Go's proxy dates one
// version at a time and fetches just the one asked about.
func (c Client) fetch(ctx context.Context, eco, name, version string) (info, error) {
	switch eco {
	case "Go":
		return c.goProxy(ctx, name, version)
	case "PyPI":
		return c.pypi(ctx, name)
	case "npm":
		return c.npm(ctx, name)
	case "crates.io":
		return c.crates(ctx, name)
	case "RubyGems":
		return c.gems(ctx, name)
	}

	return info{}, fmt.Errorf("%w: %s", errNoRegistry, eco)
}

// goProxy reads the module's @latest entry and the .info for the version under
// review, the only two answers the Go module proxy gives.
func (c Client) goProxy(ctx context.Context, name, version string) (info, error) {
	out := info{Released: map[string]time.Time{}}

	var latest proxyVersion
	if err := c.get(ctx, "Go", "/"+escapeModule(name)+"/@latest", &latest); err != nil {
		return out, err
	}

	out.Latest = latest.Version
	out.Released[latest.Version] = latest.Time
	out.Source = latest.Origin.URL

	if version != latest.Version {
		var at proxyVersion
		if err := c.get(ctx, "Go", "/"+escapeModule(name)+"/@v/"+version+".info", &at); err != nil {
			return out, err
		}

		out.Released[version] = at.Time
		if out.Source == "" {
			out.Source = at.Origin.URL
		}
	}

	if out.Source == "" {
		out.Source = goSource(name)
	}

	return out, nil
}

// proxyVersion is the Go module proxy's .info answer, whose field names are
// capitalized and whose Origin is the repository it was fetched from, where
// the proxy knows it.
//
//nolint:tagliatelle // the names are the proxy's own
type proxyVersion struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
	Origin  struct {
		URL string `json:"URL"`
	} `json:"Origin"`
}

// goSource links a module's page. A forge-hosted path is itself the
// repository; anything else resolves through pkg.go.dev, which carries the
// repository link for whatever vanity path it was imported by.
func goSource(name string) string {
	if first, _, ok := strings.Cut(name, "/"); ok && forgeHost(first) {
		return "https://" + strings.TrimSuffix(name, ".git")
	}

	return "https://pkg.go.dev/" + name
}

func forgeHost(seg string) bool {
	switch seg {
	case "github.com", "gitlab.com", "bitbucket.org":
		return true
	}

	return false
}

// escapeModule is the Go module proxy's path escaping: every uppercase letter
// becomes a bang followed by its lowercase.
func escapeModule(name string) string {
	var out strings.Builder

	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			out.WriteRune('!')
			r += 'a' - 'A'
		}

		out.WriteRune(r)
	}

	return out.String()
}

// pypi reads the package's one JSON document, which carries every release's
// upload time beside the current version and its links.
func (c Client) pypi(ctx context.Context, name string) (info, error) {
	var doc struct {
		Info struct {
			Version     string            `json:"version"`
			Summary     string            `json:"summary"`
			ProjectURLs map[string]string `json:"project_urls"`
		} `json:"info"`
		Releases map[string][]struct {
			At     string `json:"upload_time_iso_8601"`
			AtText string `json:"upload_time"`
		} `json:"releases"`
	}
	if err := c.get(ctx, "PyPI", "/pypi/"+name+"/json", &doc); err != nil {
		return info{}, err
	}

	out := info{
		Latest:   doc.Info.Version,
		Released: map[string]time.Time{},
		Summary:  doc.Info.Summary,
		Source:   firstURL(doc.Info.ProjectURLs, "Source", "Source Code", "Repository", "Code"),
	}

	for v, files := range doc.Releases {
		if len(files) == 0 {
			continue
		}

		if t := pypiTime(files[0].At, files[0].AtText); !t.IsZero() {
			out.Released[v] = t
		}
	}

	return out, nil
}

// pypiTime parses either upload field: the iso_8601 one carries a zone and the
// older one is naive UTC.
func pypiTime(at, text string) time.Time {
	for _, s := range []string{at, text} {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// npm reads the full packument, because the abbreviated form that would be a
// third the size drops the per-version dates the card exists for.
func (c Client) npm(ctx context.Context, name string) (info, error) {
	var doc struct {
		DistTags    map[string]string    `json:"dist-tags"` //nolint:tagliatelle // npm's own spelling
		Time        map[string]time.Time `json:"time"`
		Description string               `json:"description"`
		Repository  json.RawMessage      `json:"repository"`
	}
	if err := c.get(ctx, "npm", "/"+strings.Replace(name, "/", "%2F", 1), &doc); err != nil {
		return info{}, err
	}

	return info{
		Latest:   doc.DistTags["latest"],
		Released: doc.Time,
		Source:   sourceURL(doc.Repository),
		Summary:  doc.Description,
	}, nil
}

// sourceURL normalizes a repository field, which npm accepts as an object or
// a string, and writes the string several ways.
func sourceURL(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var obj struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return ""
		}

		s = obj.URL
	}

	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "git+")
	s = strings.TrimSuffix(s, ".git")

	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		s = "https://" + strings.Replace(rest, ":", "/", 1)
	}
	if rest, ok := strings.CutPrefix(s, "ssh://git@"); ok {
		s = "https://" + rest
	}

	s = strings.ReplaceAll(s, "git://", "https://")

	// "user/repo" with no host is npm's shorthand for github.com.
	if repoShort.MatchString(s) {
		s = "https://github.com/" + s
	}

	return s
}

// repoShort is npm's "user/repo" shorthand: two name-shaped pieces, no scheme
// or host.
var repoShort = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`) //nolint:gochecknoglobals // a constant

// crates reads the crate record and its version list in one answer.
func (c Client) crates(ctx context.Context, name string) (info, error) {
	var doc struct {
		Crate struct {
			Repository string `json:"repository"`
			Summary    string `json:"description"`
			MaxStable  string `json:"max_stable_version"`
			Newest     string `json:"newest_version"`
		} `json:"crate"`
		Versions []struct {
			Num    string    `json:"num"`
			At     time.Time `json:"created_at"`
			Yanked bool      `json:"yanked"`
		} `json:"versions"`
	}
	if err := c.get(ctx, "crates.io", "/api/v1/crates/"+name, &doc); err != nil {
		return info{}, err
	}

	out := info{
		Latest:   doc.Crate.MaxStable,
		Released: map[string]time.Time{},
		Source:   doc.Crate.Repository,
		Summary:  doc.Crate.Summary,
	}
	if out.Latest == "" {
		out.Latest = doc.Crate.Newest
	}

	for _, v := range doc.Versions {
		out.Released[v.Num] = v.At
	}

	return out, nil
}

// gems reads the version list for dates and the gem record for links, the two
// answers rubygems.org splits across endpoints.
func (c Client) gems(ctx context.Context, name string) (info, error) {
	var versions []struct {
		Number     string    `json:"number"`
		At         time.Time `json:"created_at"`
		Platform   string    `json:"platform"`
		Prerelease bool      `json:"prerelease"`
	}
	if err := c.get(ctx, "RubyGems", "/api/v1/versions/"+name+".json", &versions); err != nil {
		return info{}, err
	}

	var gem struct {
		Summary string `json:"info"`
		Source  string `json:"source_code_uri"`
	}
	if err := c.get(ctx, "RubyGems", "/api/v1/gems/"+name+".json", &gem); err != nil {
		return info{}, err
	}

	out := info{
		Released: map[string]time.Time{},
		Source:   gem.Source,
		Summary:  gem.Summary,
	}

	// Latest is the newest full release on the ruby platform; a prerelease is
	// not what the registry hands `gem install` without an opt-in.
	var latest time.Time

	for _, v := range versions {
		out.Released[v.Number] = v.At

		if v.Platform == "ruby" && !v.Prerelease && v.At.After(latest) {
			latest, out.Latest = v.At, v.Number
		}
	}

	return out, nil
}

func firstURL(urls map[string]string, keys ...string) string {
	for _, k := range keys {
		if urls[k] != "" {
			return urls[k]
		}
	}

	return ""
}

func (c Client) get(ctx context.Context, eco, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.at(eco, path), http.NoBody)
	if err != nil {
		return fmt.Errorf("asking the registry: %w", err)
	}

	// crates.io refuses the HTTP client's own user agent, and the others take
	// one naming the tool as a matter of politeness.
	req.Header.Set("User-Agent", "second-look (https://github.com/KyleKing/second-look)")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("asking the registry: %w", err)
	}

	//nolint:errcheck // the body is being abandoned either way
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s said %s", errRefused, req.URL.Host, res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return fmt.Errorf("reading the registry's answer: %w", err)
	}

	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("reading the registry's answer: %w", err)
	}

	return nil
}

// errNotFound is the registry answering that no such package exists, which is
// an answer the card carries rather than a failure of the ask.
var errNotFound = errors.New("the registry knows no such package")

// maxBody bounds one answer. An npm packument is the biggest of them and is
// well under this.
const maxBody = 16 << 20

func (c Client) at(eco, path string) string {
	base := c.Bases[eco]
	if base == "" {
		base = bases[eco]
	}

	return strings.TrimSuffix(base, "/") + path
}
