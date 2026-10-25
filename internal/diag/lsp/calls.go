package lsp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kyleking/second-look/internal/diag"
)

// Reasons the call graph cannot be drawn.
var (
	ErrNoCallHierarchy = errors.New("has no call hierarchy")
	ErrNoFunction      = errors.New("there is no function at that line")
)

// The protocol's symbol kinds that declare something callable.
const (
	kindMethod      = 6
	kindConstructor = 9
	kindFunction    = 12
)

// Calls is the call graph one hop either way around the function the line is
// in. The function is found from the server's own outline of the file, because
// a call hierarchy is asked at a declaration and a reviewer's cursor is on
// whatever line of the body they were reading.
func (s *Session) Calls(ctx context.Context, doc Doc, line int) (diag.Graph, error) {
	ctx, cancel := context.WithTimeout(ctx, hoverDeadline)
	defer cancel()

	at, err := s.attach(ctx, doc)
	if err != nil {
		return diag.Graph{}, err
	}

	if !at.c.callHierarchy {
		return diag.Graph{}, fmt.Errorf("%s %w", at.srv.Name, ErrNoCallHierarchy)
	}

	if n := strings.Count(doc.Text, "\n") + 1; line < 1 || line > n {
		return diag.Graph{}, fmt.Errorf("%s line %d: %w", doc.Path, line, ErrNoLine)
	}

	name, err := enclosing(ctx, at, line-1)
	if err != nil {
		return diag.Graph{}, fmt.Errorf("%s line %d: %w", doc.Path, line, err)
	}

	item, err := prepare(ctx, at, name)
	if err != nil {
		return diag.Graph{}, err
	}

	g := diag.Graph{Server: at.srv.Name, Center: s.node(item)}

	if g.Callers, err = s.hop(ctx, at, item, "callHierarchy/incomingCalls", "from"); err != nil {
		return diag.Graph{}, err
	}

	if g.Callees, err = s.hop(ctx, at, item, "callHierarchy/outgoingCalls", "to"); err != nil {
		return diag.Graph{}, err
	}

	return g, nil
}

type docSymbol struct {
	Kind           int         `json:"kind"`
	Range          lspRange    `json:"range"`
	SelectionRange lspRange    `json:"selectionRange"` //nolint:tagliatelle // the protocol's own spelling
	Children       []docSymbol `json:"children"`
}

// enclosing is where the name of the innermost function holding a row is. A
// server answering with the flat form of the outline has no selection range to
// give, so a row it holds reads as in no function.
func enclosing(ctx context.Context, at attached, row int) (position, error) {
	raw, err := at.c.call(ctx, "textDocument/documentSymbol", map[string]any{
		documentKey: map[string]any{uriKey: at.uri},
	})
	if err != nil {
		return position{}, fmt.Errorf("asking about %s: %w", at.uri, err)
	}

	var symbols []docSymbol
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return position{}, fmt.Errorf("reading the outline of %s: %w", at.uri, err)
	}

	if p, ok := innermost(symbols, row); ok {
		return p, nil
	}

	return position{}, ErrNoFunction
}

func innermost(symbols []docSymbol, row int) (position, bool) {
	for i := range symbols {
		sym := &symbols[i]
		if row < sym.Range.Start.Line || row > sym.Range.End.Line {
			continue
		}

		if p, ok := innermost(sym.Children, row); ok {
			return p, true
		}

		switch sym.Kind {
		case kindMethod, kindConstructor, kindFunction:
			return sym.SelectionRange.Start, true
		}
	}

	return position{}, false
}

// callItem is the protocol's CallHierarchyItem. Raw is the item as the server
// wrote it, which is what the next request must carry back whole: a server keeps
// its own bookkeeping in a field this client does not read.
type callItem struct {
	Name           string          `json:"name"`
	Detail         string          `json:"detail"`
	URI            string          `json:"uri"`
	Range          lspRange        `json:"range"`
	SelectionRange lspRange        `json:"selectionRange"` //nolint:tagliatelle // the protocol's own spelling
	Raw            json.RawMessage `json:"-"`
}

func (i *callItem) UnmarshalJSON(b []byte) error {
	type plain callItem

	if err := json.Unmarshal(b, (*plain)(i)); err != nil {
		return err //nolint:wrapcheck // the decoder's own error names the field
	}

	i.Raw = slices.Clone(b)

	return nil
}

func prepare(ctx context.Context, at attached, name position) (callItem, error) {
	raw, err := at.c.call(ctx, "textDocument/prepareCallHierarchy", map[string]any{
		documentKey: map[string]any{uriKey: at.uri},
		positionKey: name,
	})
	if err != nil {
		return callItem{}, fmt.Errorf("asking about %s: %w", at.uri, err)
	}

	var items []callItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return callItem{}, fmt.Errorf("reading the call hierarchy of %s: %w", at.uri, err)
	}

	if len(items) == 0 {
		return callItem{}, ErrNoFunction
	}

	return items[0], nil
}

// hop is one direction of the graph, who calls the item or whom it calls. The
// end argument names the side of each answer that is the other function.
func (s *Session) hop(ctx context.Context, at attached, item callItem, method, end string) ([]diag.CallNode, error) {
	raw, err := at.c.call(ctx, method, map[string]any{"item": item.Raw})
	if err != nil {
		return nil, fmt.Errorf("asking about %s: %w", at.uri, err)
	}

	var calls []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("reading %s: %w", method, err)
	}

	out := make([]diag.CallNode, 0, len(calls))

	for _, call := range calls {
		var (
			other  callItem
			ranges []lspRange
		)

		if err := json.Unmarshal(call[end], &other); err != nil {
			return nil, fmt.Errorf("reading %s: %w", method, err)
		}

		if err := json.Unmarshal(call["fromRanges"], &ranges); err != nil {
			return nil, fmt.Errorf("reading %s: %w", method, err)
		}

		n := s.node(other)

		// A caller's ranges are in the caller's file, and a callee's are in the
		// file that made the call.
		home := n.Path
		if end == "to" {
			home = s.path(item.URI)
		}

		for _, r := range ranges {
			n.Sites = append(n.Sites, diag.Site{Path: home, Line: r.Start.Line + 1})
		}

		out = append(out, n)
	}

	slices.SortStableFunc(out, func(a, b diag.CallNode) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line))
	})

	return out, nil
}

func (s *Session) node(i callItem) diag.CallNode {
	return diag.CallNode{
		Name: i.Name, Detail: i.Detail, Path: s.path(i.URI), Line: i.SelectionRange.Start.Line + 1,
		From: i.Range.Start.Line + 1, To: i.Range.End.Line + 1,
	}
}

// ServerName names the server that would answer for a path, and reports false
// where none is configured or installed.
func (s *Session) ServerName(path string) (string, bool) {
	srv, ok := pick(s.servers, path)

	return srv.Name, ok
}
