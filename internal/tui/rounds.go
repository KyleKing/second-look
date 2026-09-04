package tui

import (
	"strconv"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/seen"
)

// Rounds reads the diff cached at a head this review was read against, which is
// what comparing against an earlier round needs. Every round is kept for as
// long as the review is, so nothing here reaches the network.
type Rounds func(sha string) (*diff.Diff, error)

// WithRounds lets the screen compare against an earlier round. Without one the
// key says the review carries none.
func WithRounds(r Rounds) Option {
	return func(m *Model) { m.rounds = r }
}

// earlier is the rounds a comparison can be made against: every head the review
// was read at except the one it is on, newest first, since the round worth
// asking about is usually the last one.
func (m *Model) earlier() []artifact.Round {
	out := make([]artifact.Round, 0, len(m.review.Rounds))

	for i := len(m.review.Rounds) - 1; i >= 0; i-- {
		if r := m.review.Rounds[i]; r.SHA != m.review.HeadSHA {
			out = append(out, r)
		}
	}

	return out
}

// askRound opens the H chord on the rounds this review has been read at. The
// keys are digits because a round has no name of its own, and 0 is the way back
// to the whole diff.
func (m *Model) askRound() {
	if m.since != nil {
		m.clearSince()

		return
	}

	was := m.earlier()
	if len(was) == 0 {
		m.say("this review has only been read at one head, so there is no earlier round", false)

		return
	}

	m.pending = 'H'
	m.say(m.chord("H", roundHints(was, time.Now())), false)
}

// roundHints names each round by its short commit and how long ago it was read.
//
// The digit opens the label because the hint line brackets the key where it
// first appears in the text, and a commit is seven characters that may be that
// digit.
func roundHints(was []artifact.Round, now time.Time) [][2]string {
	out := make([][2]string, 0, len(was))

	for i, r := range was {
		at := strconv.Itoa(i + 1)
		out = append(out, [2]string{at, at + " " + short(r.SHA) + ", " + humanize.Ago(r.Staged, now) + " ago"})
	}

	return out
}

// sinceRound answers the second key of the H chord: what has not changed since
// the round it names is hidden, the way U hides what has already been read.
func (m *Model) sinceRound(key string) {
	was := m.earlier()

	at, err := strconv.Atoi(key)
	if err != nil || at < 1 || at > len(was) {
		m.say("no round for "+key+"; "+hintLine(styles{}, roundHints(was, time.Now())), false)

		return
	}

	if m.rounds == nil {
		m.say("this screen cannot read an earlier round's diff", true)

		return
	}

	old, err := m.rounds(was[at-1].SHA)
	if err != nil {
		m.say("could not read the diff at "+short(was[at-1].SHA)+": "+err.Error(), true)

		return
	}

	m.since = hunksOf(old)
	m.sinceSHA = was[at-1].SHA

	m.rebuild()
	m.reveal()
	m.say(sinceWord(m.sinceSHA, m.hidden()), false)
}

// hunksOf is every hunk a diff carries, by what it says rather than by where it
// sits, which is the same key a read mark uses. A hunk that survived a push
// unchanged answers to the same one in both diffs.
func hunksOf(d *diff.Diff) *seen.Set {
	out := seen.New()
	for _, ref := range seen.Hunks(d) {
		out.Mark(true, seen.Hunk(d, ref.Path, ref.Hunk))
	}

	return out
}

// clearSince puts the whole diff back.
func (m *Model) clearSince() {
	m.since, m.sinceSHA = nil, ""

	m.rebuild()
	m.reveal()
	m.say("showing every hunk again", false)
}

// sinceWord says what is left, since a diff showing three of forty hunks has to
// say why.
func sinceWord(sha string, hidden int) string {
	return "showing what changed since " + short(sha) + "; " +
		plural(hidden, "hunk") + " unchanged since then, hidden"
}

// hidden is how many hunks the comparison is holding back.
func (m *Model) hidden() int {
	if m.since == nil {
		return 0
	}

	n := 0

	for _, ref := range seen.Hunks(m.diff) {
		if m.since.Has(seen.Hunk(m.diff, ref.Path, ref.Hunk)) {
			n++
		}
	}

	return n
}
