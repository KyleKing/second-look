package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/meta"
)

// Advisor is what a lockfile can be asked about: whether anything is known
// against the versions it moved to.
//
// It is a parameter for the reason Prober is, and for one more. This is the
// only thing the screen reaches for that is not GitHub, so a review given none
// never asks, and a review given one still asks nobody until a reader says to.
type Advisor interface {
	Ask(ctx context.Context, pkgs []advisory.Package) (map[advisory.Package][]advisory.Note, error)
}

// WithAdvisor lets the review ask about a lockfile's dependencies. Without it
// the key says so rather than going quiet.
func WithAdvisor(a Advisor) Option {
	return func(m *Model) { m.advisor = a }
}

// asked is what asking one lockfile came to, which is drawn on its rows: a
// question still out, a question refused, and an answer are three different
// things and an empty card reads the same as all of them.
type asked struct {
	out     bool
	failed  string
	settled bool
	// found is how many of the file's packages something is known against,
	// which the row says: an answer of nothing and an answer of six draw the
	// same heading otherwise.
	found int
}

// advisedMsg carries one lockfile's answer.
type advisedMsg struct {
	path  string
	found map[advisory.Package][]advisory.Note
	err   error
}

// askAdvisoriesNow asks before it asks, because every package name in the
// lockfile leaves the laptop when it does. Nothing else this screen draws
// reaches anywhere but GitHub, so the reader confirms and is told what will be
// sent first.
func (m *Model) askAdvisoriesNow() {
	if m.advisor == nil {
		m.say("asking about advisories is not available here", true)

		return
	}

	file, pkgs := m.lockedAt()

	switch {
	case file == "":
		m.say("advisories are asked about a lockfile, and this is not one", true)
	case len(pkgs) == 0:
		m.say(file+" moved nothing this can ask about", true)
	case m.advised[file].out:
		m.say("already asking osv.dev about "+file+"…", false)
	default:
		m.asking = askAdvisories
		m.say(fmt.Sprintf("L again to send the name and version of %s to osv.dev, any key cancels",
			plural(len(pkgs), pkgs[0].Ecosystem+" package")), false)
	}
}

// answerAdvisories reads the reply. Anything but the same key again cancels and
// is swallowed, so no keystroke meant for the review reaches the network.
func (m *Model) answerAdvisories(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !key.Matches(msg, m.keys.Advisories) {
		m.say("canceled, nothing was sent", false)

		return m, nil
	}

	file, pkgs := m.lockedAt()
	if file == "" || len(pkgs) == 0 {
		return m, nil
	}

	m.mark(file, asked{out: true})
	m.say("asking osv.dev about "+plural(len(pkgs), "package")+"…", false)
	m.rebuild()

	ask := m.advisor

	return m, func() tea.Msg {
		found, err := ask.Ask(context.Background(), pkgs)

		return advisedMsg{path: file, found: found, err: err}
	}
}

// applyAdvisories takes one lockfile's answer. A question that failed says so
// on the file's own row: the diff is still there, and a card that quietly
// omitted a package would be worse than no card at all.
func (m *Model) applyAdvisories(msg advisedMsg) {
	if msg.err != nil {
		m.mark(msg.path, asked{failed: msg.err.Error()})
		m.say("advisories: "+msg.err.Error(), true)
		m.rebuild()

		return
	}

	if m.advisories == nil {
		m.advisories = map[advisory.Package][]advisory.Note{}
	}

	for pkg, notes := range msg.found {
		m.advisories[pkg] = notes
	}

	m.mark(msg.path, asked{settled: true, found: len(msg.found)})
	m.say(advisedWord(len(msg.found)), len(msg.found) > 0)
	m.rebuild()
}

func (m *Model) mark(path string, state asked) {
	if m.advised == nil {
		m.advised = map[string]asked{}
	}

	m.advised[path] = state
}

func advisedWord(n int) string {
	if n == 0 {
		return "osv.dev knows nothing against what this moved to"
	}

	return plural(n, "package") + " with something known against it"
}

// lockedAt is the lockfile under the cursor and the packages it moved to, which
// are the versions worth asking about: what the change moved away from is not
// what the branch installs.
func (m *Model) lockedAt() (string, []advisory.Package) {
	if m.cursor < 0 || m.cursor >= len(m.screen.rows) || m.diff == nil {
		return "", nil
	}

	path := m.screen.rows[m.cursor].path
	if path == "" {
		return "", nil
	}

	for i := range m.diff.Files {
		f := &m.diff.Files[i]
		if pathOf(f) != path {
			continue
		}

		if pkgs := packagesOf(f); pkgs != nil {
			return path, pkgs
		}

		return path, nil
	}

	return "", nil
}

func packagesOf(f *diff.File) []advisory.Package {
	eco := meta.Ecosystem(f)
	if eco == "" {
		return nil
	}

	rows, ok := meta.Read(f)
	if !ok {
		return nil
	}

	out := make([]advisory.Package, 0, len(rows))

	for _, r := range rows {
		if r.To == "" {
			continue
		}

		out = append(out, advisory.Package{Ecosystem: eco, Name: r.Name, Version: r.To})
	}

	return out
}

func pathOf(f *diff.File) string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}

// advisoryWord is what is known against one dependency. The version it was
// fixed in is the part a reviewer can act on, so it is drawn beside the name
// rather than at the end of a sentence.
func advisoryWord(n advisory.Note) string {
	parts := make([]string, 0, 4)

	if n.Severity != "" {
		parts = append(parts, n.Severity)
	}

	parts = append(parts, n.ID)

	if n.Fixed != "" {
		parts = append(parts, "fixed in "+n.Fixed)
	}

	if n.Summary != "" {
		parts = append(parts, n.Summary)
	}

	return strings.Join(parts, " · ")
}
