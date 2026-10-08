package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/kyleking/second-look/internal/diag"
)

// Definitions is where each name on one line was declared, asked of the same
// warm server a hover asks. The answer often lands outside the file, and the
// paths it lands on are read from the checkout rather than the diff, which is
// the limit this ships with: a definition in a file the checkout does not have
// is a place the server cannot point.
func (s *Session) Definitions(ctx context.Context, doc Doc, line int) ([]diag.Jump, error) {
	return s.places(ctx, doc, line, "textDocument/definition", nil)
}

// References is every place each name on one line is read. The declaration
// itself is left out, because that is the question Definitions asks.
func (s *Session) References(ctx context.Context, doc Doc, line int) ([]diag.Jump, error) {
	return s.places(ctx, doc, line, "textDocument/references",
		map[string]any{"includeDeclaration": false})
}

// places asks one method at each name on one line, keeping one of each
// answer.
func (s *Session) places(
	ctx context.Context, doc Doc, line int, method string, extra map[string]any,
) ([]diag.Jump, error) {
	srv, ok := pick(s.servers, doc.Path)
	if !ok {
		return nil, fmt.Errorf("%s: %w", doc.Path, ErrNoServer)
	}

	ctx, cancel := context.WithTimeout(ctx, hoverDeadline)
	defer cancel()

	c, err := s.serverFor(ctx, srv, srv.rootFor(s.root, doc.Path))
	if err != nil {
		return nil, err
	}

	uri := s.uri(doc.Path)
	if err := s.open(c, srv, doc.Path, uri, doc.Text); err != nil {
		return nil, fmt.Errorf("%s: %w", srv.Name, err)
	}

	lines := strings.Split(doc.Text, "\n")
	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("%s line %d: %w", doc.Path, line, ErrNoLine)
	}

	text := lines[line-1]

	var (
		out      []diag.Jump
		seen     = map[string]bool{}
		answered int
		firstErr error
	)

	for i, at := range names(text) {
		if i >= probes {
			break
		}

		params := map[string]any{
			documentKey: map[string]any{uriKey: uri},
			"position":  position{Line: line - 1, Character: unitsTo(text, at)},
		}
		if extra != nil {
			params["context"] = extra
		}

		raw, err := c.call(ctx, method, params)
		if err != nil {
			// One refused position is a name with no answer: gopls errors
			// references at a keyword where definition gets null. The refusal
			// counts only when nothing on the line was answered.
			if firstErr == nil {
				firstErr = err
			}

			continue
		}

		answered++

		sites := s.sites(raw)
		// The same name twice collapses; a different name at the same place is
		// a different answer, so the key is both.
		name := word(text, at)
		key := name + "\x00" + fmt.Sprint(sites)
		if len(sites) == 0 || seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, diag.Jump{Name: name, At: sites})
	}

	if answered == 0 && firstErr != nil {
		return nil, fmt.Errorf("asking about %s: %w", uri, firstErr)
	}

	return out, nil
}

// location is the protocol's name for a place: a file URL and a range in it.
// Only the start is kept, because a line is the granularity the screen draws.
type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

// sites reads an answer that may be a list of locations, one location, or
// null — the shapes the protocol allows a request when link support was not
// advertised in the handshake.
func (s *Session) sites(raw json.RawMessage) []diag.Site {
	var locs []location
	if err := json.Unmarshal(raw, &locs); err != nil || locs == nil {
		var one location
		if err := json.Unmarshal(raw, &one); err != nil || one.URI == "" {
			return nil
		}

		locs = []location{one}
	}

	out := make([]diag.Site, 0, len(locs))

	for _, l := range locs {
		path := s.path(l.URI)
		if path == "" {
			continue
		}

		out = append(out, diag.Site{Path: path, Line: l.Range.Start.Line + 1})
	}

	return out
}

// path is uri in reverse: the file URL back to a path. A place inside the
// checkout is spelled relative to it the way the diff spells paths, and one
// outside stays absolute because there is no shorter name for it.
func (s *Session) path(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}

	full := u.Path
	if rel, err := filepath.Rel(s.root, full); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}

	return full
}
