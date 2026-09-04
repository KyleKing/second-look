package tui

import (
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
)

// watchEvery is how often the artifact is looked at. A review is a conversation
// with an agent that takes tens of seconds to answer, so a second is soon
// enough and costs one stat.
const watchEvery = time.Second

// stamp is what says the file changed: a rewrite that keeps the size still
// moves the clock, and a write inside the same clock tick still changes the
// size for anything but a same-length replacement.
type stamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) (stamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, false
	}

	return stamp{mod: info.ModTime(), size: info.Size()}, true
}

type reloadMsg struct{ at stamp }

// watch polls the artifact so an agent's write reaches the screen the way nvim
// notices a file changed on disk, rather than being clobbered by the next thing
// typed here.
func (m *Model) watch() tea.Cmd {
	path := m.path

	return tea.Tick(watchEvery, func(time.Time) tea.Msg {
		at, ok := stampOf(path)
		if !ok {
			return reloadMsg{}
		}

		return reloadMsg{at: at}
	})
}

// headEvery is how many ticks apart the head is asked about. A push landing
// while the screen is open is worth knowing about and is not worth a request a
// second, so it is asked once a minute.
const headEvery = 60

// reloaded answers the tick. Nothing changed is the common case and costs a
// comparison. A change is read, and what it collides with is offered rather
// than resolved.
func (m *Model) reloaded(msg reloadMsg) tea.Cmd {
	m.ticks++

	// A push that lands while the screen is open is the case the check exists
	// for. It is asked again rather than only at open, because a review read
	// over twenty minutes outlives the answer given when it started.
	next := m.watch()
	if m.ticks%headEvery == 0 && m.newHead == "" {
		next = tea.Batch(next, m.checkHead())
	}

	if msg.at == (stamp{}) || msg.at == m.wrote {
		return next
	}

	// The screen's own writes move the stamp too, and the file it just wrote is
	// what it already holds.
	m.wrote = msg.at

	fresh, err := artifact.Load(m.path)
	if err != nil {
		// A half-written file is what a save in flight looks like, and the next
		// tick reads the finished one.
		return next
	}

	m.take(fresh)

	return next
}

// merge takes the review from disk. What is being typed is never overwritten:
// the buffer stays, and where the same comment moved under it both versions are
// offered rather than one of them silently winning.
func (m *Model) take(fresh *artifact.Review) {
	said := arrived(m.review, fresh)

	if m.editing == nil {
		m.review = fresh
		m.rebuild()
		m.say(said, false)

		return
	}

	index := m.editing.msg.index
	if index < 0 || index >= len(m.review.Comments) {
		m.review = fresh
		m.rebuild()
		m.say(said, false)

		return
	}

	was := m.review.Comments[index]
	m.review = fresh

	theirs := fresh.Find(was.ID)
	if theirs == nil || theirs.Body == was.Body {
		m.rebuild()
		m.say(said, false)

		return
	}

	// The collision: the comment being typed in was rewritten underneath. The
	// buffer wins the screen and the other version is held on ctrl+t, because
	// resolving it here would throw away one of the two without asking.
	m.editing.theirs = theirs.Body
	m.rebuild()
	m.say(was.ID+" was rewritten on disk while you were in it; ctrl+t reads that version", false)
}

// arrived says what the reload brought, since a screen that redraws with no
// word about why reads as a glitch.
func arrived(was, now *artifact.Review) string {
	turns := 0

	for i := range now.Comments {
		old := was.Find(now.Comments[i].ID)
		if old == nil {
			continue
		}

		turns += len(now.Comments[i].Turns) - len(old.Turns)
	}

	switch {
	case turns > 0:
		return fmt.Sprintf("the review changed on disk: %s arrived", plural(turns, "turn"))
	case len(now.Comments) != len(was.Comments):
		return fmt.Sprintf("the review changed on disk: %s now", plural(len(now.Comments), "comment"))
	}

	return "the review changed on disk and was reloaded"
}
