package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// movedKey answers a key whose job moved this release: it says where the job
// went, and for ctrl+r, which still restages for one release, does the job
// too. A bare t or c off a thread was the threads or comments view; t stays
// the todo verdict on a finding, so its notice is written here rather than
// read from the keymap, where it would collide with that verdict.
//
// A state key is one rule on every row: bare r, d, t, or x stamps the comment
// under the cursor the way it rules on a thread, and m then the same letter
// does the same. It reaches here only off a thread, since judgeKey answers a
// thread first.
func (m *Model) movedKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()

	if m.stateKey(msg) && m.current() >= 0 {
		m.remember(0, msg)
		_, cmd := m.act(msg)

		return cmd, true
	}

	switch k {
	case "t":
		m.say(`t moved to \t (threads); m t marks a comment todo`, false)
		m.j.Gated(ctxReview, k)

		return nil, true
	case "u":
		m.say(`u moved: the looks are under \ now (\g \s \w \i \p, \\ the next view); u is undo`, false)
		m.j.Gated(ctxReview, k)

		return nil, true
	}

	mv, ok := moveNotice(m.km, k)
	if !ok {
		// Off a comment and off a thread a state key has nothing to stamp,
		// which is said rather than ignored.
		if m.stateKey(msg) {
			m.say(k+" sets the state of a comment or thread, and the cursor is on neither", false)

			return nil, true
		}

		return nil, false
	}

	m.j.Gated(ctxReview, k)

	if k == ctrlR {
		cmd := m.askRestage()
		m.say(mv.Notice+"; ctrl+r still restages this release", false)

		return cmd, true
	}

	m.say(mv.Notice, false)

	return nil, true
}

// lookKey answers the \ chord: what the diff is drawn with, the views that sit
// off the cycle, and a second \ for the next view.
func (m *Model) lookKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "i":
		return m.previewImages()
	case "t":
		m.showThreads()
	case "c":
		m.showComments()
	case lookKey:
		m.cycleView()
	default:
		m.toggleLook(msg)
	}

	return nil
}

// branchKey answers the b chord: the branches and their checks, or deleting
// this one, which asks first.
func (m *Model) branchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "b":
		return m.openDashboard()
	case "d":
		m.askDeleteBranchNow()
	default:
		m.say("no branch verb for "+msg.String()+"; "+hintLine(branchObjects()), false)
	}

	return nil
}

// yank copies where the cursor stands as path:line, the reference a chat or
// an editor takes, through the terminal's clipboard.
func (m *Model) yank() tea.Cmd {
	path, line := m.siteUnderCursor()
	if path == "" {
		m.say("nothing to yank here; Y copies a line of the diff as path:line", false)

		return nil
	}

	ref := path
	if line > 0 {
		ref += ":" + strconv.Itoa(line)
	}

	m.say("yanked "+ref, false)

	return tea.SetClipboard(ref)
}
