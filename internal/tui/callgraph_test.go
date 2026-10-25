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

	for _, tt := range []struct {
		name      string
		callers   []tui.GraphNode
		callees   []tui.GraphNode
		onCallees bool
		at, width int
		want      []string
		row       int
	}{
		{
			name: "nothing around it", width: 60,
			want: []string{" no callers          ▶ sync ✎ ◆       no callees"},
		},
		{
			name:    "one each way",
			callers: []tui.GraphNode{{Name: "run", Times: 1, Edited: true}},
			callees: []tui.GraphNode{node("pages", "intake/pages.py:12")},
			width:   70,
			want:    []string{"›run ✎   ─────▶ sync ✎ ◆ ───── pages     intake/pages.py:12"},
		},
		{
			name:    "folded and selected",
			callers: []tui.GraphNode{node("run", ""), {Name: "backfill", Times: 3, Judge: true}},
			callees: []tui.GraphNode{
				node("pages", "intake/pages.py:12"),
				{Name: "fetch", Times: 1, Edited: true, Where: "intake/client.py:40"},
				node("backoff", "intake/sync.py:142"), node("a", "x.py:1"), node("b", "x.py:2"),
				node("log.warn", "x.py:3"), node("metrics.inc", "x.py:4"),
			},
			onCallees: true, at: 1, width: 80, row: 1,
			want: []string{
				"                                   ┌── pages       intake/pages.py:12",
				"                                   ├──›fetch   ✎   intake/client.py:40",
				" run             ──┐               ├── backoff     intake/sync.py:142",
				"                   ├──▶ sync ✎ ◆ ──┤",
				" backfill ×3   ◆ ──┘               ├── a           x.py:1",
				"                                   ├── b           x.py:2",
				"                                   └── +2          log.warn, metrics.inc",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, row := tui.DrawGraph(sync, tt.callers, tt.callees, tt.onCallees, tt.at, tt.width)
			if !slices.Equal(got, tt.want) || row != tt.row {
				t.Errorf("laid out at %d\n%s\nwant at %d\n%s",
					row, strings.Join(got, "\n"), tt.row, strings.Join(tt.want, "\n"))
			}
		})
	}
}
