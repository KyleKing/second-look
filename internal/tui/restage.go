package tui

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"
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
// touched comes back. Everything else the screen holds is about rows that no
// longer exist, so the folds and the cursor go back to where an open starts.
func (m *Model) applyRestaged(msg restagedMsg) {
	if msg.err != nil {
		m.say("could not restage: "+msg.err.Error(), true)

		return
	}

	was := len(m.review.Comments)

	m.review = msg.fresh.Review
	m.diff = msg.fresh.Diff
	m.threads = msg.fresh.Threads
	m.read = msg.fresh.Read
	m.newHead = ""
	m.fold = foldNone
	m.folded = newFolded()
	m.notes = nil
	m.cursor = 0

	m.rebuild()
	m.reveal()

	m.say(restagedWord(was, len(m.review.Comments), msg.fresh.HeadSHA), false)
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
