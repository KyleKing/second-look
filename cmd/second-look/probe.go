package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/config"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diag/lsp"
	"github.com/kyleking/second-look/internal/diag/scan"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// prober answers the screen's questions about the files under review: what a
// language server and the configured checks make of them, and what the names on
// one line are.
//
// It reads each file through the same reader the context keys use, which reads
// the commit rather than the working tree. What a server resolves around that
// file still comes from disk, so a checkout left on another branch answers for
// the commit in the file it was handed and for the branch in everything else.
type prober struct {
	session *lsp.Session
	read    blob.Reader
	root    string
	files   []string
	checks  []scan.Check
}

// probeFor builds the prober for a review, and nil where nothing could answer:
// no checkout to run in, or no server installed and no check configured.
//
// A prober that would answer nothing is worse than none, because the screen
// offers the key either way and an empty trouble list would read as a clean
// change rather than as a pass that never ran.
//
//nolint:ireturn // Prober is the seam the screen depends on; concrete would remove it
func probeFor(ctx context.Context, root string, d *diff.Diff, reader blob.Reader) tui.Prober {
	if root == "" {
		return nil
	}

	cfg, err := loadConfig()
	if err != nil {
		cfg = &config.Config{}
	}

	files := diag.Files(d)
	servers := servers(cfg)
	checks := checks(cfg)

	if len(checks) == 0 && !lsp.Answers(servers, files) {
		return nil
	}

	return &prober{
		session: lsp.New(ctx, root, servers),
		read:    reader, root: root, files: files, checks: checks,
	}
}

// Notes is what every checker has to say about the review's files.
func (p *prober) Notes(ctx context.Context) ([]diag.Note, error) {
	docs, errs := p.docs(ctx)

	notes, err := p.session.Notes(ctx, docs)
	errs = append(errs, err)

	found, err := scan.Run(ctx, p.root, p.checks, p.files)
	errs = append(errs, err)

	return append(notes, found...), errors.Join(errs...)
}

// Hover is what each name on one line is.
func (p *prober) Hover(ctx context.Context, path string, line int) ([]diag.Symbol, error) {
	lines, err := p.read.Read(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	syms, err := p.session.Hover(ctx, lsp.Doc{Path: path, Text: strings.Join(lines, "\n")}, line)
	if err != nil {
		return nil, fmt.Errorf("asking about %s: %w", path, err)
	}

	return syms, nil
}

func (p *prober) Close() { p.session.Close() }

// docs is every file under review as it reads after the change. A file that
// cannot be read is reported and skipped: one file of a diff being unreadable
// is not a reason to check none of it.
func (p *prober) docs(ctx context.Context) ([]lsp.Doc, []error) {
	var (
		docs []lsp.Doc
		errs []error
	)

	for _, path := range p.files {
		lines, err := p.read.Read(ctx, path)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading %s: %w", path, err))

			continue
		}

		docs = append(docs, lsp.Doc{Path: path, Text: strings.Join(lines, "\n")})
	}

	return docs, errs
}

// servers is the configured servers ahead of the built-in ones, so an extension
// named in the config wins the one that claims it here.
func servers(cfg *config.Config) []lsp.Server {
	out := make([]lsp.Server, 0, len(cfg.Servers)+len(lsp.Builtin()))

	for i := range cfg.Servers {
		s := &cfg.Servers[i]
		out = append(out, lsp.Server{
			Name: s.Name, Argv: s.Command, Exts: s.Extensions, Language: s.Language,
			Roots: ranked(s.Roots), Needs: s.Needs, Settings: s.Settings,
		})
	}

	return append(out, lsp.Builtin()...)
}

// ranked reads a configured marker list as one group per marker, which is what
// makes the order it is written in the order it is believed.
func ranked(roots []string) [][]string {
	out := make([][]string, 0, len(roots))

	for _, r := range roots {
		out = append(out, []string{r})
	}

	return out
}

func checks(cfg *config.Config) []scan.Check {
	out := make([]scan.Check, 0, len(cfg.Checks))

	for _, c := range cfg.Checks {
		out = append(out, scan.Check{
			Name: c.Name, Command: c.Command, Format: scan.Format(c.Format),
			Roots: ranked(c.Roots), Bin: c.Bin,
		})
	}

	return out
}
