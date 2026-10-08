package versions_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kyleking/aragonite/cache"

	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/versions"
)

// serve stands up a registry and reports how many requests it answered, so a
// test can prove the cache did the second asking. A path the routes do not
// name answers 404, which is the registry saying it knows no such package.
func serve(t *testing.T, routes map[string]string) (string, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		body, ok := routes[r.URL.EscapedPath()]
		if !ok {
			http.Error(w, "no such route", http.StatusNotFound)

			return
		}

		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("writing the answer: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv.URL, &hits
}

func ask(t *testing.T, eco, base, name, version string) versions.Card {
	t.Helper()

	p := advisory.Package{Ecosystem: eco, Name: name, Version: version}

	cards, err := (versions.Client{Bases: map[string]string{eco: base}}).Cards(
		context.Background(), []advisory.Package{p},
	)
	if err != nil {
		t.Fatalf("cards: %v", err)
	}

	return cards[p]
}

// wantCard checks the four facts a card carries. Latest and Released are what
// the row is drawn out of; Source and Summary are the detail under it.
func wantCard(t *testing.T, card versions.Card, latest string, released time.Time, source, summary string) {
	t.Helper()

	if card.Latest != latest {
		t.Errorf("latest = %q, want %q", card.Latest, latest)
	}
	if !card.Released.Equal(released) {
		t.Errorf("released = %v, want %v", card.Released, released)
	}
	if card.Source != source {
		t.Errorf("source = %q, want %q", card.Source, source)
	}
	if card.Summary != summary {
		t.Errorf("summary = %q, want %q", card.Summary, summary)
	}
}

// asked records the path a request went out on, which is how a test proves a
// name was escaped on the wire.
type asked struct {
	v atomic.Value
}

func (a *asked) handler(t *testing.T, body string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		a.v.Store(r.URL.EscapedPath())
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("writing the answer: %v", err)
		}
	}
}

func (a *asked) path(t *testing.T) string {
	t.Helper()

	p, ok := a.v.Load().(string)
	if !ok {
		t.Fatal("the registry was never asked")
	}

	return p
}

// forget drops a test's package from the shared card memory, which is what a
// repeated run (-count=2) needs to count requests honestly.
func forget(eco string, names ...string) {
	for _, name := range names {
		versions.Forget(eco, name)
	}
}

func TestGoProxy(t *testing.T) {
	t.Parallel()

	forget("Go", "x/mod")

	base, hits := serve(t, map[string]string{
		"/x/mod/@latest":        `{"Version":"v0.9.0","Time":"2026-03-01T12:00:00Z"}`,
		"/x/mod/@v/v0.8.1.info": `{"Version":"v0.8.1","Time":"2026-02-01T12:00:00Z"}`,
	})

	wantCard(t, ask(t, "Go", base, "x/mod", "v0.8.1"),
		"v0.9.0", time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC), "https://pkg.go.dev/x/mod", "")

	if hits.Load() != 2 {
		t.Errorf("the proxy answered %d requests, want 2", hits.Load())
	}
}

// A module path's uppercase escapes as !lower, and a path that is itself a
// forge's names its repository rather than a pkg.go.dev page.
func TestGoEscapingAndForgeSource(t *testing.T) {
	t.Parallel()

	forget("Go", "github.com/BurntSushi/toml")

	var saw asked

	srv := httptest.NewServer(saw.handler(t, `{"Version":"v1.4.0","Time":"2026-01-01T00:00:00Z"}`))
	t.Cleanup(srv.Close)

	card := ask(t, "Go", srv.URL, "github.com/BurntSushi/toml", "v1.4.0")

	if got := saw.path(t); got != "/github.com/!burnt!sushi/toml/@latest" {
		t.Errorf("the module path was not escaped: %s", got)
	}
	if card.Source != "https://github.com/BurntSushi/toml" {
		t.Errorf("source = %q", card.Source)
	}
}

func TestPyPI(t *testing.T) {
	t.Parallel()

	base, _ := serve(t, map[string]string{
		"/pypi/httpx/json": `{
			"info": {
				"version": "0.28.1",
				"summary": "The next generation HTTP client.",
				"project_urls": {"Source": "https://github.com/encode/httpx"}
			},
			"releases": {
				"0.28.1": [{"upload_time_iso_8601": "2025-12-01T10:00:00Z"}],
				"0.28.0": [{"upload_time": "2025-11-15T10:00:00"}],
				"0.0.1": []
			}
		}`,
	})

	wantCard(t, ask(t, "PyPI", base, "httpx", "0.28.0"),
		"0.28.1", time.Date(2025, 11, 15, 10, 0, 0, 0, time.UTC),
		"https://github.com/encode/httpx", "The next generation HTTP client.")
}

// The packument carries the dates and the repository, which a registry spells
// several ways; a scoped name escapes its slash on the way out.
func TestNpm(t *testing.T) {
	t.Parallel()

	for i, tc := range []struct {
		repository string
		source     string
	}{
		{
			`{"type": "git", "url": "git+https://github.com/microsoft/TypeScript.git"}`,
			"https://github.com/microsoft/TypeScript",
		},
		{`{"url": "git@github.com:microsoft/TypeScript.git"}`, "https://github.com/microsoft/TypeScript"},
		{`{"url": "git://github.com/microsoft/TypeScript"}`, "https://github.com/microsoft/TypeScript"},
		{`{"url": "ssh://git@github.com/microsoft/TypeScript.git"}`, "https://github.com/microsoft/TypeScript"},
		{`"microsoft/TypeScript"`, "https://github.com/microsoft/TypeScript"},
		{`{"url": "https://gitlab.com/microsoft/TypeScript"}`, "https://gitlab.com/microsoft/TypeScript"},
	} {
		name := fmt.Sprintf("typescript-%d", i)
		base, _ := serve(t, map[string]string{
			"/" + name: `{
				"dist-tags": {"latest": "5.9.2", "next": "6.0.0-rc.1"},
				"time": {"5.9.1": "2025-07-20T00:00:00.000Z", "5.9.2": "2025-08-01T00:00:00.000Z"},
				"description": "TypeScript is a language for application scale JavaScript",
				"repository": ` + tc.repository + `
			}`,
		})

		card := ask(t, "npm", base, name, "5.9.1")

		if card.Latest != "5.9.2" {
			t.Errorf("%s: latest = %q, and not the prerelease dist-tag", tc.repository, card.Latest)
		}
		if card.Source != tc.source {
			t.Errorf("%s: source = %q, want %q", tc.repository, card.Source, tc.source)
		}
	}
}

// The packument the card needs is the full one: a scoped name's request path
// proves the slash escaped, and the version dates are the `time` map.
func TestNpmScopedPathAndDates(t *testing.T) {
	t.Parallel()

	forget("npm", "@scope/typescript-pkg")

	var saw asked

	srv := httptest.NewServer(saw.handler(t, `{
		"dist-tags": {"latest": "5.9.2"},
		"time": {"5.9.1": "2025-07-20T00:00:00.000Z"}
	}`))
	t.Cleanup(srv.Close)

	card := ask(t, "npm", srv.URL, "@scope/typescript-pkg", "5.9.1")

	if got := saw.path(t); got != "/@scope%2Ftypescript-pkg" {
		t.Errorf("the scoped name was not escaped: %s", got)
	}
	if want := time.Date(2025, 7, 20, 0, 0, 0, 0, time.UTC); !card.Released.Equal(want) {
		t.Errorf("released = %v, want %v", card.Released, want)
	}
}

func TestCrates(t *testing.T) {
	t.Parallel()

	base, _ := serve(t, map[string]string{
		"/api/v1/crates/serde": `{
			"crate": {
				"repository": "https://github.com/serde-rs/serde",
				"description": "A generic serialization/deserialization framework",
				"max_stable_version": "1.0.219",
				"newest_version": "1.0.220-beta.1"
			},
			"versions": [
				{"num": "1.0.219", "created_at": "2025-01-01T00:00:00Z", "yanked": false},
				{"num": "1.0.218", "created_at": "2024-12-01T00:00:00Z", "yanked": false}
			]
		}`,
	})

	wantCard(t, ask(t, "crates.io", base, "serde", "1.0.218"),
		"1.0.219", time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
		"https://github.com/serde-rs/serde", "A generic serialization/deserialization framework")
}

// Latest is what `gem install` would get, which is the newest full release on
// the ruby platform rather than the newest record.
func TestGems(t *testing.T) {
	t.Parallel()

	base, _ := serve(t, map[string]string{
		"/api/v1/versions/rake.json": `[
			{"number": "13.2.1", "platform": "ruby", "created_at": "2025-04-01T00:00:00Z", "prerelease": false},
			{"number": "14.0.0.beta1", "platform": "ruby", "created_at": "2025-05-01T00:00:00Z", "prerelease": true},
			{"number": "13.1.0", "platform": "ruby", "created_at": "2024-10-01T00:00:00Z", "prerelease": false}
		]`,
		"/api/v1/gems/rake.json": `{
			"info": "Rake is a Make-like program implemented in Ruby",
			"source_code_uri": "https://github.com/ruby/rake"
		}`,
	})

	wantCard(t, ask(t, "RubyGems", base, "rake", "13.1.0"),
		"13.2.1", time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC),
		"https://github.com/ruby/rake", "Rake is a Make-like program implemented in Ruby")
}

// A 404 is the registry's own answer, so it is a Missing card rather than a
// failure, and a cached one rather than a re-ask.
func TestMissingIsAnAnswerNotAFailure(t *testing.T) {
	t.Parallel()

	forget("npm", "no-such-package")

	base, hits := serve(t, map[string]string{})

	p := advisory.Package{Ecosystem: "npm", Name: "no-such-package", Version: "1.0.0"}
	c := versions.Client{Bases: map[string]string{"npm": base}}

	cards, err := c.Cards(context.Background(), []advisory.Package{p})
	if err != nil {
		t.Fatalf("a 404 is an answer, not a failure: %v", err)
	}
	if !cards[p].Missing {
		t.Error("the card does not say the registry knows no such package")
	}

	if _, err := c.Cards(context.Background(), []advisory.Package{p}); err != nil {
		t.Fatalf("the cached miss came back as a failure: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("a cached miss re-asked the registry: %d requests", hits.Load())
	}
}

// An answer cached at one version covers another, because every registry but
// the Go proxy dates them all at once.
func TestCardsReadsTheCache(t *testing.T) {
	t.Parallel()

	forget("npm", "cached")

	base, hits := serve(t, map[string]string{
		"/cached": `{
			"dist-tags": {"latest": "2.0.0"},
			"time": {"1.0.0": "2025-01-01T00:00:00Z", "2.0.0": "2025-06-01T00:00:00Z"}
		}`,
	})
	c := versions.Client{Bases: map[string]string{"npm": base}}

	if _, err := c.Cards(context.Background(),
		[]advisory.Package{{Ecosystem: "npm", Name: "cached", Version: "1.0.0"}}); err != nil {
		t.Fatalf("first ask: %v", err)
	}

	cards, err := c.Cards(context.Background(), []advisory.Package{
		{Ecosystem: "npm", Name: "cached", Version: "1.0.0"},
		{Ecosystem: "npm", Name: "cached", Version: "2.0.0"},
	})
	if err != nil {
		t.Fatalf("second ask: %v", err)
	}

	if hits.Load() != 1 {
		t.Errorf("the cache let a second ask reach the registry: %d requests", hits.Load())
	}
	got := cards[advisory.Package{Ecosystem: "npm", Name: "cached", Version: "2.0.0"}]
	if !got.Released.Equal(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("a second review at another version did not read the packument already cached")
	}
}

// A failed ask is neither an answer nor a card, and nothing about it is kept.
func TestAFailedAskIsAnError(t *testing.T) {
	t.Parallel()

	forget("npm", "down")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	p := advisory.Package{Ecosystem: "npm", Name: "down", Version: "1.0.0"}
	c := versions.Client{Bases: map[string]string{"npm": srv.URL}}

	cards, err := c.Cards(context.Background(), []advisory.Package{p})
	if err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("the failure did not name the package: %v", err)
	}
	if _, ok := cards[p]; ok {
		t.Error("a failed ask drew a card")
	}

	if _, err := c.Cards(context.Background(), []advisory.Package{p}); err == nil {
		t.Fatal("a failure was cached as an answer")
	}
}

func TestAnEcosystemWithNoRegistry(t *testing.T) {
	t.Parallel()

	_, err := (versions.Client{}).Cards(context.Background(),
		[]advisory.Package{{Ecosystem: "Hex", Name: "plug", Version: "1.0.0"}})
	if err == nil || !strings.Contains(err.Error(), "Hex") {
		t.Fatalf("an ecosystem with no registry answered %v", err)
	}
}

// The disk store is what carries an answer into the next process, which here
// is this one with its memory of the package dropped.
func TestDiskCacheCarriesBetweenRuns(t *testing.T) {
	t.Parallel()

	base, hits := serve(t, map[string]string{
		"/disk": `{"dist-tags": {"latest": "1.0.0"}, "time": {"1.0.0": "2025-01-01T00:00:00Z"}}`,
	})
	c := versions.Client{Bases: map[string]string{"npm": base}}
	p := advisory.Package{Ecosystem: "npm", Name: "disk", Version: "1.0.0"}

	forget("npm", "disk")

	//nolint:usetesting // the store is a process global, so a parallel test's
	// persist can land in it while TempDir's RemoveAll runs and fail the test
	dir, err := os.MkdirTemp("", "second-look-versions-")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cache.SetDiskCache(nil)
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("a racing persist left the store behind: %v", err)
		}
	})

	cache.SetDiskCache(cache.NewDiskCache(dir))

	if _, err := c.Cards(context.Background(), []advisory.Package{p}); err != nil {
		t.Fatalf("first ask: %v", err)
	}

	versions.Forget("npm", "disk")

	if _, err := c.Cards(context.Background(), []advisory.Package{p}); err != nil {
		t.Fatalf("second ask: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("the disk store did not carry the answer between runs: %d requests", hits.Load())
	}
}
