package tui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/tui"
)

func TestDrawGraph(t *testing.T) {
	t.Parallel()

	node := func(name, where string) tui.GraphNode { return tui.GraphNode{Name: name, Times: 1, Where: where} }
	sync := tui.GraphNode{Name: "sync", Edited: true, Judge: true}

	handshake, six := tui.GraphNode{Name: "handshake", Edited: true}, sixCallees()
	serverFor := []tui.GraphNode{node("serverFor", "")}

	for _, tt := range []struct {
		name      string
		center    tui.GraphNode
		callers   []tui.GraphNode
		callees   []tui.GraphNode
		onCallees bool
		at, width int
		want      []string
		row       int
	}{
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
				"                                      ┌── ›Getpid          os • exec.go",
				"                                      ├──  Base            path/filepath • path.go",
				" serverFor     ─────▶ handshake ✎   ──┼──  call            internal/diag/lsp/client.go:231",
				"                                      ├──  notify ×2       internal/diag/lsp/client.go:310",
				"                                      ├──  offersCalls ✎   internal/diag/lsp/session.go:428",
				"                                      └──  +1              fileURI",
			}, center: handshake, callers: serverFor, callees: six, onCallees: true, width: 100,
		},
		{
			name: "one caller, six callees, medium",
			want: []string{
				"                                      ┌──  Getpid          os • exec.go",
				"                                      ├──  Base            …/filepath • path.go",
				"›serverFor     ─────▶ handshake ✎   ──┼──  call            …/lsp/client.go:231",
				"                                      ├──  notify ×2       …/lsp/client.go:310",
				"                                      ├──  offersCalls ✎   …/lsp/session.go:428",
				"                                      └──  +1              fileURI",
			}, row: 2, center: handshake, callers: serverFor, callees: six, width: 80,
		},
		{
			name: "one caller, six callees, narrow",
			want: []string{
				"                                      ┌──  Getpid",
				"                                      ├──  Base",
				" serverFor     ─────▶ handshake ✎   ──┼── ›call",
				"                                      ├──  notify ×2",
				"                                      ├──  offersCa… ✎",
				"                                      └──  +1",
			}, row: 2, center: handshake, callers: serverFor, callees: six, width: 56, at: 2, onCallees: true,
		},
		{
			name: "two callers, seven callees",
			want: []string{
				"                                   ┌──  Getpid        os • exec.go",
				"                                   ├── ›Base          path/filepath • path.go",
				" run             ──┬──▶ sync ✎ ◆ ──┼──  call          …/diag/lsp/client.go:231",
				" backfill ×3   ◆ ──┘               ├──  notify ×2     …/diag/lsp/client.go:310",
				"                                   ├──  a             x.py:1",
				"                                   └──  +2            b, c",
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, row := tui.DrawGraph(tt.center, tt.callers, tt.callees, tt.onCallees, tt.at, tt.width)
			if !slices.Equal(got, tt.want) || row != tt.row {
				t.Errorf("laid out at %d\n%s\nwant at %d\n%s",
					row, strings.Join(got, "\n"), tt.row, strings.Join(tt.want, "\n"))
			}
		})
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
