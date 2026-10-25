package main

import (
	"context"
	"errors"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/labels"
	"github.com/kyleking/second-look/internal/people"
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

func roster(root string, r *artifact.Review) tui.Roster {
	repo := r.Owner + "/" + r.Repo

	return func(ctx context.Context) ([]string, []string, error) {
		involved, onPR := people.Involved(ctx, root, repo, r.Number)
		recent, onRepo := people.Recent(ctx, root, repo)

		return involved, recent, errors.Join(onPR, onRepo)
	}
}
