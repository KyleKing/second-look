package main

import (
	"context"

	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/labels"
	"github.com/kyleking/second-look/internal/tui"
)

func labeling(root string) tui.Labeling {
	return tui.Labeling{
		Read: func(ctx context.Context, repo string) ([]labels.Label, error) {
			return labels.Read(ctx, root, repo)
		},
		Set: func(ctx context.Context, at labels.Target, name string, on bool) error {
			return labels.Set(ctx, ghrun.GH(), root, at, name, on)
		},
	}
}
