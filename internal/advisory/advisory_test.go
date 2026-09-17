package advisory_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/advisory"
)

// osv stands in for the API. The batch call answers ids alone, which is the
// shape the real one has and the reason an advisory is read a second time.
func osv(t *testing.T, hits map[string][]string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, "/v1/vulns/"); ok {
			write(t, w, map[string]any{
				"id": id, "summary": "Prototype Pollution in " + id,
				"aliases":           []string{"CVE-2020-8203"},
				"database_specific": map[string]any{"severity": "HIGH"},
				"affected": []map[string]any{{"ranges": []map[string]any{{
					"events": []map[string]any{{"introduced": "3.7.0"}, {"fixed": "4.17.19"}},
				}}}},
			})

			return
		}

		var asked struct {
			Queries []struct {
				Package struct {
					Name string `json:"name"`
				} `json:"package"`
			} `json:"queries"`
		}

		if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
			t.Errorf("decoding the query: %v", err)
		}

		results := make([]any, 0, len(asked.Queries))

		for _, q := range asked.Queries {
			vulns := make([]any, 0)
			for _, id := range hits[q.Package.Name] {
				vulns = append(vulns, map[string]any{"id": id})
			}

			results = append(results, map[string]any{"vulns": vulns})
		}

		write(t, w, map[string]any{"results": results})
	}))

	t.Cleanup(srv.Close)

	return srv
}

func write(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("writing the answer: %v", err)
	}
}

// The batch call carries ids and nothing else, so what a reviewer can act on
// (what it is, how bad, and the version it was fixed in) is a second read per
// advisory. A package nothing is known against is absent rather than empty.
func TestAskReadsWhatEachAdvisorySaysAndLeavesTheCleanOut(t *testing.T) {
	t.Parallel()

	srv := osv(t, map[string][]string{"lodash": {"GHSA-p6mc-m468-83gw"}})
	c := advisory.Client{Base: srv.URL}

	bad := advisory.Package{Ecosystem: "npm", Name: "lodash", Version: "4.17.15"}
	good := advisory.Package{Ecosystem: "npm", Name: "left-pad", Version: "1.3.0"}

	got, err := c.Ask(t.Context(), []advisory.Package{bad, good})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	if _, ok := got[good]; ok {
		t.Errorf("a package nothing is known against came back as %+v", got[good])
	}

	notes := got[bad]
	if len(notes) != 1 {
		t.Fatalf("read %+v, want the one advisory against it", notes)
	}

	want := advisory.Note{
		ID: "GHSA-p6mc-m468-83gw", Summary: "Prototype Pollution in GHSA-p6mc-m468-83gw",
		Severity: "high", Fixed: "4.17.19", Aliases: []string{"CVE-2020-8203"},
	}

	if notes[0].ID != want.ID || notes[0].Severity != want.Severity ||
		notes[0].Fixed != want.Fixed || notes[0].Summary != want.Summary {
		t.Errorf("the advisory is\n  %+v\nwant\n  %+v", notes[0], want)
	}
}

// Being offline, being rate limited, and a private registry all read the same
// way, and a card that quietly omitted a package would be worse than no card.
func TestAskSaysWhenTheAnswerWasNotOne(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := advisory.Client{Base: srv.URL}

	_, err := c.Ask(t.Context(), []advisory.Package{{Ecosystem: "npm", Name: "lodash", Version: "4.17.15"}})
	if !errors.Is(err, advisory.ErrRefused) {
		t.Errorf("a refused question answered %v, want it named as refused", err)
	}
}

// The answer is positional, so one shorter than the question cannot be lined up
// with what was asked: reading it in order would attribute an advisory to a
// package it was not about.
func TestAskRefusesAnAnswerThatDoesNotLineUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		write(t, w, map[string]any{"results": []any{map[string]any{"vulns": []any{}}}})
	}))
	t.Cleanup(srv.Close)

	c := advisory.Client{Base: srv.URL}

	_, err := c.Ask(t.Context(), []advisory.Package{
		{Ecosystem: "npm", Name: "lodash", Version: "4.17.15"},
		{Ecosystem: "npm", Name: "left-pad", Version: "1.3.0"},
	})
	if !errors.Is(err, advisory.ErrRefused) {
		t.Errorf("a short answer was read as %v, want it refused", err)
	}
}

// Two databases publish the same finding under their own ids and name each
// other as aliases, so a Go module's hit arrives twice and reads as two
// problems where there is one.
func TestAskSaysTheSameFindingOnce(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, "/v1/vulns/"); ok {
			write(t, w, map[string]any{
				"id": id, "summary": "the same finding",
				"aliases": []string{"CVE-2020-28483", "GHSA-h395-qcrw-5vmq", "GO-2021-0052"},
			})

			return
		}

		write(t, w, map[string]any{"results": []any{map[string]any{"vulns": []any{
			map[string]any{"id": "GHSA-h395-qcrw-5vmq"},
			map[string]any{"id": "GO-2021-0052"},
		}}}})
	}))
	t.Cleanup(srv.Close)

	gin := advisory.Package{Ecosystem: "Go", Name: "github.com/gin-gonic/gin", Version: "v1.6.0"}

	got, err := advisory.Client{Base: srv.URL}.Ask(t.Context(), []advisory.Package{gin})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	if len(got[gin]) != 1 {
		t.Errorf("read %+v, want one finding rather than each database's name for it", got[gin])
	}
}
