package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/seen"
)

// Rounds reads the diff a head this review was read against carried, which is
// what comparing against an earlier round needs. The cache answers while it
// holds one; a head the pull request moved past is rebuilt from the forge, so
// the call can take a network round trip.
type Rounds func(ctx context.Context, sha string) (*diff.Diff, error)

// WithRounds lets the screen compare against an earlier round. Without one the
// key says the review carries none.
func WithRounds(r Rounds) Option {
	return func(m *Model) { m.rounds = r }
}

// numbered is a round with its place in the review's history. The number is how
// a round is named on screen and in the chord, so it stays the same as later
// rounds arrive.
type numbered struct {
	n int
	artifact.Round
}

// earlier is the rounds a comparison can be made against: every head the review
// was read at except the one it is on, oldest first.
func (m *Model) earlier() []numbered {
	out := make([]numbered, 0, len(m.review.Rounds))

	for i, r := range m.review.Rounds {
		if r.SHA != m.review.HeadSHA {
			out = append(out, numbered{n: i + 1, Round: r})
		}
	}

	return out
}

// askRound opens the H chord on the rounds this review has been read at. The
// keys are digits because a round has no name of its own, and a second H is
// the way back to the whole diff. The diffs the box counts answers from are
// read behind the frame and fill the box in as they land.
func (m *Model) askRound() tea.Cmd {
	if m.since != nil {
		m.clearSince()

		return nil
	}

	was := m.earlier()
	if len(was) == 0 {
		m.say("this review has only been read at one head, so there is no earlier round", false)

		return nil
	}

	m.pending = 'H'
	m.say(m.chord("H", roundKeys(was)), false)

	return m.readRounds(was)
}

// roundKeys names each round by its number and short commit.
//
// The digit opens the label because the hint line brackets the key where it
// first appears in the text, and a commit is seven characters that may be that
// digit.
func roundKeys(was []numbered) [][2]string {
	out := make([][2]string, 0, len(was))

	for _, r := range was {
		at := strconv.Itoa(r.n)
		out = append(out, [2]string{at, at + " " + short(r.SHA)})
	}

	return out
}

// roundCols is the H chord's rows as columns: the round's number, short commit,
// age, and what is known of who pushed it and how the todos stand.
func (m *Model) roundCols(now time.Time) [][4]string {
	was := m.earlier()
	out := make([][4]string, 0, len(was))

	for _, r := range was {
		out = append(out, [4]string{
			strconv.Itoa(r.n), short(r.SHA), when(r.Staged, now), strings.Join(m.roundFacts(r), " · "),
		})
	}

	return out
}

// roundPage is the rounds as the legend's hints, one line each.
func (m *Model) roundPage(now time.Time) [][2]string {
	cols := m.roundCols(now)
	out := make([][2]string, 0, len(cols))

	for _, c := range cols {
		out = append(out, [2]string{c[0], strings.TrimSpace(strings.Join(c[1:], " "))})
	}

	return out
}

// roundGaps is the gutters between the four columns of a round row.
const roundGaps = 3

// roundRows draws the H chord's box: one unwrapped row per round, the age padded
// so the columns line up, and the last column cut with an ellipsis to fit width.
func (m *Model) roundRows(width int) []string {
	cols := m.roundCols(time.Now())
	aged := 0

	for _, c := range cols {
		aged = max(aged, textWidth(c[2]))
	}

	out := make([]string, 0, len(cols))

	for _, c := range cols {
		fixed := textWidth(c[0]) + textWidth(c[1]) + aged + roundGaps*len(keyhint.Gutter)
		line := m.styles.key.Render(c[0]) + keyhint.Gutter + m.styles.footer.Render(c[1]+keyhint.Gutter+
			table.Pad(c[2], aged, table.AlignLeft)+keyhint.Gutter+table.Truncate(c[3], max(1, width-fixed)))
		out = append(out, strings.TrimRight(line, " "))
	}

	return out
}

// when is a round's age as a reader says it: relative while recent, a date after.
func when(at, now time.Time) string {
	const week = 7 * 24 * time.Hour

	switch {
	case at.IsZero():
		return ""
	case now.Sub(at) < week:
		return humanize.Ago(at, now) + " ago"
	}

	return at.Format("Jan 2")
}

// roundFacts is what is known about a round without asking the forge: who
// pushed it and how many commits it held, from the commits the screen already
// read, and how many todos it answers, from the round's own cached diff.
func (m *Model) roundFacts(r numbered) []string {
	var out []string

	author, n := m.pushedIn(r)
	if author != "" {
		out = append(out, author)
	}

	if n > 0 {
		out = append(out, plural(n, "commit"))
	}

	if old := m.roundDiffs[r.SHA]; old != nil {
		if done, all := answeredOf(m.answersSince(old)); all > 0 {
			out = append(out, fmt.Sprintf("%d of %d todos answered", done, all))
		}
	}

	return out
}

// pushedIn is who wrote a round's head commit and how many commits the round
// added to the one before it. Both are empty where the commits are not known,
// as after a force push that dropped the head.
func (m *Model) pushedIn(r numbered) (string, int) {
	at := m.commitAt(r.SHA)
	if at < 0 {
		return "", 0
	}

	from := -1

	if r.n > 1 {
		if from = m.commitAt(m.review.Rounds[r.n-2].SHA); from < 0 || from >= at {
			return m.commits[at].Author, 0
		}
	}

	return m.commits[at].Author, at - from
}

func (m *Model) commitAt(sha string) int {
	return slices.IndexFunc(m.commits, func(c plan.Commit) bool { return c.ID == sha })
}

// readRounds reads the diff of every round not yet held, so the box can say
// how the todos stand against each. The cache answers most of them.
func (m *Model) readRounds(was []numbered) tea.Cmd {
	if m.rounds == nil {
		return nil
	}

	var cmds []tea.Cmd

	for _, r := range was {
		if m.roundDiffs[r.SHA] == nil && !m.roundAsked[r.SHA] {
			m.roundAsked[r.SHA] = true
			cmds = append(cmds, m.readRound(r.SHA, false))
		}
	}

	return tea.Batch(cmds...)
}

func (m *Model) readRound(sha string, pick bool) tea.Cmd {
	ctx, read := m.ctx, m.rounds

	return func() tea.Msg {
		old, err := read(ctx, sha)

		return roundMsg{sha: sha, diff: old, err: err, pick: pick}
	}
}

// sinceRound answers the second key of the H chord: what has not changed since
// the round it names is hidden, the way U hides what has already been read.
// The round's diff can be a fetch away, so it is read behind the frame rather
// than on the key.
func (m *Model) sinceRound(key string) tea.Cmd {
	was := m.earlier()

	at, err := strconv.Atoi(key)
	i := slices.IndexFunc(was, func(r numbered) bool { return r.n == at })

	if err != nil || i < 0 {
		m.say("no round for "+key+"; "+hintLine(roundKeys(was)), false)

		return nil
	}

	sha := was[i].SHA

	if m.roundDiffs[sha] != nil {
		m.compareTo(sha)

		return nil
	}

	if m.rounds == nil {
		m.say("this screen cannot read an earlier round's diff", true)

		return nil
	}

	m.say("reading the round at "+short(sha)+"…", false)

	return m.readRound(sha, true)
}

// roundMsg is the diff an earlier round was read against.
type roundMsg struct {
	sha  string
	diff *diff.Diff
	err  error
	// pick marks a read the reader asked for by choosing the round, as opposed
	// to one the box made to count its answers.
	pick bool
}

func (m *Model) applyRound(msg roundMsg) {
	delete(m.roundAsked, msg.sha)

	if msg.err != nil {
		if msg.pick {
			m.say("could not read the diff at "+short(msg.sha)+": "+msg.err.Error(), true)
		}

		return
	}

	m.roundDiffs[msg.sha] = msg.diff

	if msg.pick {
		m.compareTo(msg.sha)
	}
}

// compareTo hides every hunk the round already carried.
func (m *Model) compareTo(sha string) {
	m.since = hunksOf(m.roundDiffs[sha])
	m.sinceSHA = sha

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

// sinceHeader is the header's account of a comparison: which round, and how
// many of the todos handed back since have been dealt with.
func (m *Model) sinceHeader() string {
	if m.since == nil {
		return ""
	}

	word := "since round " + strconv.Itoa(m.roundNumber(m.sinceSHA))
	if done, all := answeredOf(m.rereview); all > 0 {
		word += fmt.Sprintf(" · %d of %d todos answered", done, all)
	}

	return word
}

func (m *Model) roundNumber(sha string) int {
	return slices.IndexFunc(m.review.Rounds, func(r artifact.Round) bool { return r.SHA == sha }) + 1
}

// rereviewed is how each todo thread stands against the round being compared,
// and nothing outside a comparison.
func (m *Model) rereviewed() map[int]answer {
	if m.since == nil || m.roundDiffs[m.sinceSHA] == nil {
		return nil
	}

	return m.answersSince(m.roundDiffs[m.sinceSHA])
}
