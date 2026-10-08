package main

import (
	"context"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/checkouts"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/lease"
	"github.com/kyleking/second-look/internal/tui"
)

// cloneNote says which clone C would move for the focused repository and what
// state it is in, so the key is not a surprise. The dashboard is a subprocess
// over its own cache, so the answer arrives after the frame it was asked in.
func cloneNote(ctx context.Context, repo string) tea.Cmd {
	return func() tea.Msg {
		return tui.FocusNote{Repo: repo, Note: cloneWord(ctx, repo)}
	}
}

func cloneWord(ctx context.Context, repo string) string {
	if ours := lease.Ours(get.Host, repo); ours != nil {
		return filepath.Base(ours.Record.Path) + " leased"
	}

	found, err := checkouts.Find(ctx, checkouts.Dashboard(), repo, "")
	if err != nil {
		return humanize.FirstLine(err.Error())
	}

	held := lease.List(get.Host, repo)

	var free []checkouts.Checkout
	for i := range found {
		if _, ok := held[found[i].Path]; !ok {
			free = append(free, found[i])
		}
	}

	if len(free) == 0 {
		if len(found) > 0 {
			return "every clone is leased"
		}

		return "no clone here"
	}

	best := &free[0]

	word := filepath.Base(best.Path)
	if best.Dirty {
		return word + " has uncommitted changes"
	}

	return word + " clean"
}
