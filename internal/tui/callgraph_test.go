package tui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/tui"
)

type graphCase struct {
	name      string
	center    tui.GraphNode
	callers   []tui.GraphNode
	callees   []tui.GraphNode
	onCallees bool
	via, at   int
	width     int
	want      []string
	row       int
}

var (
	syncNode  = tui.GraphNode{Name: "sync", Edited: true, Judge: true}
	handshake = tui.GraphNode{Name: "handshake", Edited: true}
	serverFor = []tui.GraphNode{graphNode("serverFor", "")}
)

func graphNode(name, where string) tui.GraphNode {
	return tui.GraphNode{Name: name, Times: 1, Where: where}
}

func TestDrawGraph(t *testing.T) {
	t.Parallel()

	for _, tt := range slices.Concat(drawnGraphs(), scrolledGraphs()) {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, row := tui.DrawGraph(tt.center, tt.callers, tt.callees, tt.onCallees, tt.via, tt.at, tt.width)
			if !slices.Equal(got, tt.want) || row != tt.row {
				t.Errorf("laid out at %d\n%s\nwant at %d\n%s",
					row, strings.Join(got, "\n"), tt.row, strings.Join(tt.want, "\n"))
			}
		})
	}
}

func drawnGraphs() []graphCase {
	node, sync, six := graphNode, syncNode, sixCallees()

	return []graphCase{
		{
			name: "nothing around it",
			want: []string{
				" no callers          ▶ sync ✎ ◆        no callees",
			}, center: sync, width: 60,
		},
		{
			name: "one each way",
			want: []string{
				"›run ✎   ─────▶ sync ✎ ◆ ─────  pages     intake/pages.py:12",
			}, center: sync, width: 70,
			callers: []tui.GraphNode{{Name: "run", Times: 1, Edited: true}},
			callees: []tui.GraphNode{node("pages", "intake/pages.py:12")},
		},
		{
			name: "one caller, six callees, wide",
			want: []string{
				"                                      ┌── ›Getpid        os • exec.go",
				"                                      ├──  Base          path/filepath • path.go",
				" serverFor     ─────▶ handshake ✎   ──┼──  call          internal/diag/lsp/client.go:231",
				"                                      ├──  notify ×2     internal/diag/lsp/client.go:310",
				"                                      └──  ↓ 2 more      offersCalls, fileURI",
			}, center: handshake, callers: serverFor, callees: six, onCallees: true, width: 100,
		},
		{
			name: "one caller, six callees, medium",
			want: []string{
				"                                      ┌──  Getpid        os • exec.go",
				"                                      ├──  Base          path/filepath • path.go",
				"›serverFor     ─────▶ handshake ✎   ──┼──  call          …/lsp/client.go:231",
				"                                      ├──  notify ×2     …/lsp/client.go:310",
				"                                      └──  ↓ 2 more      offersCalls, fileURI",
			}, row: 2, center: handshake, callers: serverFor, callees: six, width: 80,
		},
		{
			name: "one caller, six callees, narrow",
			want: []string{
				"                                      ┌──  Getpid",
				"                                      ├──  Base",
				" serverFor     ─────▶ handshake ✎   ──┼── ›call",
				"                                      ├──  notify ×2",
				"                                      └──  ↓ 2 more",
			}, row: 2, center: handshake, callers: serverFor, callees: six, width: 56, at: 2, onCallees: true,
		},
		{
			name: "two callers, seven callees",
			want: []string{
				"                                   ┌──  Getpid        os • exec.go",
				"                                   ├── ›Base          path/filepath • path.go",
				" run             ──┬──▶ sync ✎ ◆ ──┼──  call          …/diag/lsp/client.go:231",
				" backfill ×3   ◆ ──┘               ├──  notify ×2     …/diag/lsp/client.go:310",
				"                                   └──  ↓ 3 more      a, b, c",
			}, row: 1, center: sync, width: 80, onCallees: true, at: 1,
			callers: []tui.GraphNode{node("run", ""), {Name: "backfill", Times: 3, Judge: true}},
			callees: append(slices.Clone(six[:4]), node("a", "x.py:1"), node("b", "x.py:2"), node("c", "x.py:3")),
		},
		{
			name: "four callers, one callee",
			want: []string{
				"›a     ──┐",
				" b     ──┼──▶ sync ✎ ◆ ─────  pages     p.py:1",
				" c     ──┤",
				" d     ──┘",
			}, center: sync, width: 70,
			callers: []tui.GraphNode{node("a", ""), node("b", ""), node("c", ""), node("d", "")},
			callees: []tui.GraphNode{node("pages", "p.py:1")},
		},
	}
}

// scrolledGraphs are the sides longer than the five rows they draw, after the
// selection has walked down to via and back to at.
func scrolledGraphs() []graphCase {
	node, sync, six := graphNode, syncNode, sixCallees()

	return []graphCase{
		{
			name: "six callees, scrolled to the end", via: 5, at: 5, onCallees: true, center: handshake,
			callers: serverFor, callees: six, width: 100, row: 4, want: []string{
				"                                      ┌──  ↑ 2 more        Getpid, Base",
				"                                      ├──  call            internal/diag/lsp/client.go:231",
				" serverFor     ─────▶ handshake ✎   ──┼──  notify ×2       internal/diag/lsp/client.go:310",
				"                                      ├──  offersCalls ✎   internal/diag/lsp/session.go:428",
				"                                      └── ›fileURI         x.go:1",
			},
		},
		{
			name: "six callees, back up inside the window", via: 4, at: 3, onCallees: true, center: handshake,
			callers: serverFor, callees: six, width: 100, row: 2, want: []string{
				"                                      ┌──  ↑ 2 more        Getpid, Base",
				"                                      ├──  call            internal/diag/lsp/client.go:231",
				" serverFor     ─────▶ handshake ✎   ──┼── ›notify ×2       internal/diag/lsp/client.go:310",
				"                                      ├──  offersCalls ✎   internal/diag/lsp/session.go:428",
				"                                      └──  fileURI         x.go:1",
			},
		},
		{
			name: "seven callers, scrolled to the middle", via: 5, at: 5, center: sync, width: 70, row: 3,
			callers: []tui.GraphNode{
				node("a", ""), node("b", ""), node("c", ""), node("d", ""), node("e", ""), node("f", ""), node("g", ""),
			},
			callees: []tui.GraphNode{node("pages", "p.py:1")}, want: []string{
				" ↑ 3 more a, b, c     ──┐",
				" d                    ──┤",
				" e                    ──┼──▶ sync ✎ ◆ ─────  pages     p.py:1",
				"›f                    ──┤",
				" g                    ──┘",
			},
		},
	}
}

// sixCallees are what handshake calls, two of them outside the checkout.
func sixCallees() []tui.GraphNode {
	node := func(name, where string) tui.GraphNode { return tui.GraphNode{Name: name, Times: 1, Where: where} }

	return []tui.GraphNode{
		node("Getpid", "os • exec.go"), node("Base", "path/filepath • path.go"),
		node("call", "internal/diag/lsp/client.go:231"),
		{Name: "notify", Times: 2, Where: "internal/diag/lsp/client.go:310"},
		{Name: "offersCalls", Times: 1, Edited: true, Where: "internal/diag/lsp/session.go:428"},
		node("fileURI", "x.go:1"),
	}
}
