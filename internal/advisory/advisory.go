// Package advisory asks [OSV] what is known against the versions a lockfile
// moved to.
//
// A lockfile diff is hundreds of lines that say almost nothing, and the one
// question in it that changes a review decision is whether any of what moved is
// known to be vulnerable. OSV answers a whole lockfile in one request, with no
// key and no account, which is what makes this the half of the lockfile card
// worth reaching the network for.
//
// Nothing here is asked unless a reader asks for it. Every package name in a
// lockfile leaves the laptop when it is, which is a change in what the tool is
// and belongs to the person reading rather than to the screen.
//
// [OSV]: https://osv.dev
package advisory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Endpoint is the public OSV API, which is what a Client with no base uses.
const Endpoint = "https://api.osv.dev"

// ErrRefused reports an answer that was not one, which is what being offline,
// being rate limited, and a private registry all read as.
var ErrRefused = errors.New("osv.dev did not answer")

// Package is one dependency at one version, named the way OSV indexes it.
type Package struct {
	// Ecosystem is OSV's own name for the index: Go, npm, PyPI, crates.io,
	// RubyGems.
	Ecosystem string
	Name      string
	Version   string
}

// Note is one advisory against one package.
type Note struct {
	// ID is the advisory's own, which osv.dev resolves as a page.
	ID      string
	Summary string
	// Severity is the word the record carries, and empty where it carries
	// none. It is not computed from the CVSS vector, because a number this
	// invented would read as the publisher's.
	Severity string
	// Fixed is the first version the package is no longer affected in, and
	// empty where the advisory names none.
	Fixed string
	// Aliases are the same advisory's other names, CVE among them.
	Aliases []string
}

// Client asks OSV. The zero value asks the public endpoint through the default
// HTTP client.
type Client struct {
	HTTP *http.Client
	// Base is the API root, which a test points at its own server.
	Base string
}

// batch is how many packages OSV takes in one query, which is its own limit.
const batch = 1000

// details is how many advisories are read at once. An advisory is one small
// request and a lockfile rarely hits more than a handful, so this is a guard
// against a bad day rather than a throughput setting.
const details = 8

// timeout bounds the whole question. A lockfile answers in well under a second
// against the real endpoint, so anything past this is a network that is not
// there rather than one that is slow.
const timeout = 20 * time.Second

// Ask is what is known against each of these packages.
//
// A package with nothing against it is absent from the answer rather than
// present and empty. OSV says the same thing about a package it has never heard
// of as about one it has and finds clean, so this cannot report which names it
// failed to resolve.
func (c Client) Ask(ctx context.Context, pkgs []Package) (map[Package][]Note, error) {
	out := map[Package][]Note{}
	if len(pkgs) == 0 {
		return out, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	hits, err := c.query(ctx, pkgs)
	if err != nil {
		return nil, err
	}

	known, err := c.read(ctx, ids(hits))
	if err != nil {
		return nil, err
	}

	for pkg, held := range hits {
		notes := make([]Note, 0, len(held))

		for _, id := range held {
			if note, ok := known[id]; ok {
				notes = append(notes, note)
			}
		}

		if notes = distinct(notes); len(notes) > 0 {
			out[pkg] = notes
		}
	}

	return out, nil
}

// distinct drops an advisory another one already says. Two databases publish
// the same finding under their own ids and name each other as aliases, so a Go
// module hit reads twice: once as GO- and once as GHSA-.
func distinct(notes []Note) []Note {
	out := make([]Note, 0, len(notes))
	said := map[string]bool{}

	for _, n := range notes {
		if said[n.ID] {
			continue
		}

		out = append(out, n)

		said[n.ID] = true

		for _, alias := range n.Aliases {
			said[alias] = true
		}
	}

	return out
}

// query is the batch call, which answers advisory ids and nothing else.
func (c Client) query(ctx context.Context, pkgs []Package) (map[Package][]string, error) {
	out := map[Package][]string{}

	for chunk := range slices.Chunk(pkgs, batch) {
		asked := make([]any, 0, len(chunk))

		for _, p := range chunk {
			asked = append(asked, map[string]any{
				"package": map[string]any{"name": p.Name, "ecosystem": p.Ecosystem},
				"version": p.Version,
			})
		}

		var answer struct {
			Results []struct {
				Vulns []struct {
					ID string `json:"id"`
				} `json:"vulns"`
			} `json:"results"`
		}

		if err := c.post(ctx, "/v1/querybatch", map[string]any{"queries": asked}, &answer); err != nil {
			return nil, err
		}

		// The answer is positional, so a short one cannot be lined up with what
		// was asked and is not worth guessing at.
		if len(answer.Results) != len(chunk) {
			return nil, fmt.Errorf("%w: it answered %d of %d packages",
				ErrRefused, len(answer.Results), len(chunk))
		}

		for i, r := range answer.Results {
			for _, v := range r.Vulns {
				out[chunk[i]] = append(out[chunk[i]], v.ID)
			}
		}
	}

	return out, nil
}

// ids is every advisory named once, in order, so one named against four
// packages is read once.
func ids(hits map[Package][]string) []string {
	var out []string

	seen := map[string]bool{}

	for _, held := range hits {
		for _, id := range held {
			if !seen[id] {
				seen[id] = true

				out = append(out, id)
			}
		}
	}

	slices.Sort(out)

	return out
}

// read fetches each advisory, because the batch call carries ids alone and what
// a reviewer needs is the summary and the version it was fixed in.
func (c Client) read(ctx context.Context, want []string) (map[string]Note, error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		out  = map[string]Note{}
		errs []error
		gate = make(chan struct{}, details)
	)

	for _, id := range want {
		wg.Add(1)

		go func() {
			defer wg.Done()

			gate <- struct{}{}
			defer func() { <-gate }()

			note, err := c.one(ctx, id)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errs = append(errs, err)

				return
			}

			out[id] = note
		}()
	}

	wg.Wait()

	return out, errors.Join(errs...)
}

func (c Client) one(ctx context.Context, id string) (Note, error) {
	var raw record

	if err := c.get(ctx, "/v1/vulns/"+url.PathEscape(id), &raw); err != nil {
		return Note{}, err
	}

	return raw.note(), nil
}

// record is the part of an OSV advisory a review reads.
type record struct {
	ID       string   `json:"id"`
	Summary  string   `json:"summary"`
	Details  string   `json:"details"`
	Aliases  []string `json:"aliases"`
	Affected []struct {
		Ranges []struct {
			Events []struct {
				Fixed string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

func (r record) note() Note {
	summary := r.Summary
	if summary == "" {
		summary = firstLine(r.Details)
	}

	return Note{
		ID: r.ID, Summary: summary, Aliases: r.Aliases,
		Severity: strings.ToLower(r.DatabaseSpecific.Severity),
		Fixed:    r.fixed(),
	}
}

// fixed is the first version named as fixing this, which is the one thing an
// advisory says that a reviewer can act on.
func (r record) fixed() string {
	for _, a := range r.Affected {
		for _, rng := range a.Ranges {
			for _, e := range rng.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}

	return ""
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	return strings.TrimSpace(line)
}

func (c Client) post(ctx context.Context, path string, body, into any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("asking osv.dev: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.at(path), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("asking osv.dev: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return c.send(req, into)
}

func (c Client) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.at(path), http.NoBody)
	if err != nil {
		return fmt.Errorf("asking osv.dev: %w", err)
	}

	return c.send(req, into)
}

func (c Client) send(req *http.Request, into any) error {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}

	//nolint:gosec // the host is this package's own endpoint or a test's
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}

	//nolint:errcheck // the body is being abandoned either way
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s said %s", ErrRefused, req.URL.Path, res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}

	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}

	return nil
}

// maxBody bounds one answer. The batch reply to a thousand packages is the
// largest of them and is well under this.
const maxBody = 16 << 20

func (c Client) at(path string) string {
	base := c.Base
	if base == "" {
		base = Endpoint
	}

	return strings.TrimSuffix(base, "/") + path
}
