package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/versions"
)

// Versions asks a package's registry what a lockfile change is reading: when
// the version moved to shipped and what the registry calls current.
type Versions interface {
	Cards(ctx context.Context, pkgs []advisory.Package) (map[advisory.Package]versions.Card, error)
}

// WithVersions gives the review a way to ask registries about the packages a
// lockfile moved. The asking needs no consent the advisory check does: a card
// names nothing the lockfile does not already carry. Without one the rows
// carry only the versions the diff moved between.
func WithVersions(v Versions) Option {
	return func(m *Model) { m.versions = v }
}

// versionedMsg carries one lockfile's registry answers.
type versionedMsg struct {
	path  string
	cards map[advisory.Package]versions.Card
	err   error
}

// fetchVersions asks every lockfile's registries about the versions it moved
// to, one command per file so a failed registry says so on its own file's
// heading rather than a neighbor's.
func (m *Model) fetchVersions() []tea.Cmd {
	if m.versions == nil || m.diff == nil {
		return nil
	}

	ask := m.versions

	var cmds []tea.Cmd

	for i := range m.diff.Files {
		f := &m.diff.Files[i]
		if pkgs := packagesOf(f); len(pkgs) > 0 {
			path := pathOf(f)
			m.markVersioned(path, asked{out: true})

			cmds = append(cmds, func() tea.Msg {
				cards, err := ask.Cards(context.Background(), pkgs)

				return versionedMsg{path: path, cards: cards, err: err}
			})
		}
	}

	return cmds
}

// applyVersions takes one lockfile's answers. Cards that arrived draw even
// where others failed, since a package missing its card is drawn as one that
// never got an answer rather than one the registry knows nothing about.
func (m *Model) applyVersions(msg versionedMsg) {
	for pkg, card := range msg.cards {
		if m.cards == nil {
			m.cards = map[advisory.Package]versions.Card{}
		}

		m.cards[pkg] = card
	}

	if msg.err != nil {
		m.markVersioned(msg.path, asked{failed: msg.err.Error()})
	} else {
		m.markVersioned(msg.path, asked{settled: true, found: len(msg.cards)})
	}

	m.rebuild()
}

func (m *Model) markVersioned(path string, state asked) {
	if m.versioned == nil {
		m.versioned = map[string]asked{}
	}

	m.versioned[path] = state
}

// versionedWord is what the lockfile's own row says about the asking: a fetch
// still out and a registry that refused read differently from an answer of
// rows, which says nothing more.
func versionedWord(state asked) string {
	switch {
	case state.out:
		return " · asking registries…"
	case state.failed != "":
		return " · " + state.failed
	}

	return ""
}

// cardWord is what a registry said about the version a dependency moved to,
// beside the versions the diff moved between.
func cardWord(c versions.Card, has bool, eco string) string {
	if !has {
		return ""
	}

	if c.Missing {
		return " · not in " + eco
	}

	var parts []string

	if !c.Released.IsZero() {
		parts = append(parts, "released "+humanize.Ago(c.Released, time.Now())+" ago")
	}
	if c.Latest != "" {
		parts = append(parts, "latest "+c.Latest)
	}

	if len(parts) == 0 {
		return ""
	}

	return " · " + strings.Join(parts, " · ")
}

// cardDetail is the line under a dependency's card: the registry's one-line
// description and where the source lives, where it carries either.
func cardDetail(c versions.Card) string {
	if c.Summary != "" && c.Source != "" {
		return c.Summary + " · " + c.Source
	}

	return c.Summary + c.Source
}
