package tui

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/rate"
	"github.com/kyleking/second-look/internal/structure"
)

// restagedMsg is what preparing the review again answered.
type restagedMsg struct {
	fresh *Restaged
	err   error
}

// askRestage prepares the review again against the head the pull request is on
// now, off the frame, since it is three API calls.
func (m *Model) askRestage() tea.Cmd {
	if m.restage == nil {
		m.say("this review cannot restage itself; second-look get "+
			strconv.Itoa(m.review.Number)+" does it", true)

		return nil
	}

	if m.newHead == "" {
		m.say("nothing to restage: this diff is the head the pull request is on", false)

		return nil
	}

	m.say("restaging against "+short(m.newHead)+"…", false)

	restage := m.restage

	return func() tea.Msg {
		fresh, err := restage(context.WithoutCancel(m.ctx))

		return restagedMsg{fresh: fresh, err: err}
	}
}

// applyRestaged swaps in the review prepared against the new head.
//
// The read marks are keyed by what a hunk says rather than by the commit it sat
// on, so a hunk that survived the push unchanged stays read and one that was
// touched comes back. Everything else the screen holds is about the old diff:
// the highlight and word-mark caches keyed by line number would draw the old
// head's coloring onto a line that kept its numbering, and the rating, the
// found trouble, and the blob reader were all read against it. Each is dropped
// or re-read, and the folds and the cursor go back to where an open starts.
func (m *Model) applyRestaged(msg restagedMsg) tea.Cmd {
	if msg.err != nil {
		m.say("could not restage: "+msg.err.Error(), true)

		return nil
	}

	was := len(m.review.Comments)

	m.review = msg.fresh.Review
	m.diff = msg.fresh.Diff
	m.refined = m.diff.Refine()
	m.lexed = map[string]map[diff.LineRef][]highlight.Span{}
	m.threads = msg.fresh.Threads
	m.read = msg.fresh.Read
	m.newHead = ""
	m.fold = foldNone
	m.folded = newFolded()
	m.notes = nil
	m.cursor = 0
	m.around = map[hunkAt]int{}
	m.blobs = map[string][]string{}
	if msg.fresh.Blobs != nil {
		m.blob = msg.fresh.Blobs
	}

	if m.prober != nil {
		m.prober.Close()
	}
	m.prober, m.trouble, m.troubled = msg.fresh.Prober, diag.Placed{}, nil

	m.cosmetic, m.shape = nil, shape{}
	m.cost, m.size = rate.Score{}, rate.Size{}

	m.rebuild()
	m.reveal()

	m.say(restagedWord(was, len(m.review.Comments), msg.fresh.HeadSHA), false)

	cmds := []tea.Cmd{m.probe(), m.probeAgent()}
	if structure.Available() {
		cmds = append(cmds, readStructure(m.diff, m.made))
	}

	return tea.Batch(cmds...)
}

// restagedWord says what survived, because a restage that silently dropped a
// staged comment would be the worst thing this key could do.
func restagedWord(was, now int, sha string) string {
	word := "restaged against " + short(sha)
	if was != now {
		return word + "; " + plural(was-now, "comment") + " no longer anchors in this diff"
	}

	return word + "; every staged comment still anchors"
}
