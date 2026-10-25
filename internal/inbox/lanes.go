package inbox

import (
	"fmt"
	"slices"
	"strings"
)

// Lane is where a row sits in the default queue, ordered by how much of the
// work is yours to do now.
type Lane int

// The lanes, in the order they are drawn.
const (
	LaneReady Lane = iota
	LaneBack
	LaneNew
	LaneWaiting
	LaneMerged
)

const (
	approved         = "APPROVED"
	changesRequested = "CHANGES_REQUESTED"
)

var laneNames = [...]string{"ready to merge", "back to you", "new for you", "waiting on others", "merged"}

func (l Lane) String() string { return laneNames[l] }

// MarshalText spells the lane by name in --json.
//
//nolint:unparam // encoding.TextMarshaler fixes the signature
func (l Lane) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// Source is which search a row came from.
type Source uint8

// The searches the default queue makes.
const (
	Requested Source = 1 << iota
	Reviewed
	Authored
	Merged
)

// Context is what ranking a row needs beyond the row.
type Context struct {
	Viewer string
	// Priority are label names that put a row first, compared without case.
	Priority []string
	// Area are the top-level directories of the pull request opened last.
	Area map[string]bool
}

// Tone is how loud a row's status reads, which the screen maps to a color so
// that nothing here knows what a terminal's warning looks like.
type Tone uint8

// The tones, quietest first. ToneNone is a row with nothing to say.
const (
	ToneNone Tone = iota
	ToneMuted
	ToneGood
	ToneNew
	ToneWarn
	ToneBad
	ToneMerged
)

// ShortWidth is the widest a Judgement's Short word may be, so a status cell of
// a glyph, a space, and ShortWidth cells always holds it whole.
const ShortWidth = 14

// Judgement is what a row's lane, columns, and ordering signals come to. The
// three booleans are only ever set in LaneNew.
//
// Why is the prose that --json and the plain queue print. Glyph, Short, Long,
// and Tone say the same thing to a screen, which draws the glyph in the tone's
// color and picks Short or Long by the room it has. They are empty together.
type Judgement struct {
	Lane     Lane   `json:"lane"`
	Who      string `json:"who,omitempty"`
	Why      string `json:"why,omitempty"`
	Priority bool   `json:"priority,omitempty"`
	Alone    bool   `json:"alone,omitempty"`
	Area     bool   `json:"area,omitempty"`
	Glyph    string `json:"-"`
	Short    string `json:"-"`
	Long     string `json:"-"`
	Tone     Tone   `json:"-"`
}

// status is one reason a row sits where it does, in the three spellings its
// readers need.
type status struct {
	why, glyph, short, long string
	tone                    Tone
}

func (j *Judgement) say(s status) {
	j.Why, j.Glyph, j.Short, j.Long, j.Tone = s.why, s.glyph, s.short, s.long, s.tone
}

// same is a status whose short and long words are its prose.
func same(why, glyph string, tone Tone) status {
	return status{why: why, glyph: glyph, short: why, long: why, tone: tone}
}

var (
	statusMerged      = same("merged", "✓", ToneMerged)
	statusApproved    = same("approved", "✓", ToneGood)
	statusReady       = status{"approved, checks passing", "✓", "approved", "approved, checks passing", ToneGood}
	statusAwaiting    = status{"awaiting review", "○", "awaiting", "awaiting review", ToneMuted}
	statusDraft       = same("draft", "◌", ToneMuted)
	statusChecksFail  = same("checks failing", "✗", ToneBad)
	statusChecksWait  = same("checks pending", "○", ToneMuted)
	statusAsked       = status{"changes requested", "✗", "changes asked", "changes requested", ToneBad}
	statusYouAsked    = status{"you asked for changes", "✗", "changes asked", "you asked for changes", ToneBad}
	statusPushed      = status{"pushed since your review", "↻", "new commits", "pushed since your review", ToneNew}
	statusYouApproved = same("you approved", "✓", ToneMuted)
	statusYouComment  = same("you commented", "○", ToneMuted)
	statusYouReviewed = same("you reviewed", "○", ToneMuted)
	statusPriority    = status{"priority", "", "priority", "priority label", ToneWarn}
	statusArea        = same("same area", "", ToneMuted)
)

func comments(n int) status {
	if n == 1 {
		return status{"1 new comment", "◆", "1 comment", "1 new comment", ToneWarn}
	}

	return status{
		fmt.Sprintf("%d new comments", n), "◆",
		fmt.Sprintf("%d comments", n), fmt.Sprintf("%d new comments", n), ToneWarn,
	}
}

// Mergeable reports a row the viewer can merge from the queue.
func (j Judgement) Mergeable() bool { return j.Lane == LaneReady }

// Judge places one row. Without facts it answers from the search alone, which is
// provisional: the row moves when the facts land.
func Judge(p *PullRequest, via Source, f *Facts, c Context) Judgement {
	switch {
	case via&Merged != 0:
		j := Judgement{Lane: LaneMerged}
		j.say(statusMerged)

		return j
	case f == nil:
		return provisional(via)
	}

	j := Judgement{Who: who(f.Requested, c.Viewer)}

	if p.Author == c.Viewer {
		var s status

		j.Lane, s = authored(p, f, c.Viewer)
		j.say(s)

		return j
	}

	var s status

	j.Lane, s = others(via, f, c.Viewer)
	j.say(s)

	if j.Lane == LaneNew {
		j.signal(p, f, c)
	}

	return j
}

func provisional(via Source) Judgement {
	if via&Requested != 0 && via&Authored == 0 {
		return Judgement{Lane: LaneNew}
	}

	return Judgement{Lane: LaneWaiting}
}

func (j *Judgement) signal(p *PullRequest, f *Facts, c Context) {
	j.Priority = slices.ContainsFunc(p.Labels, func(l string) bool {
		return slices.ContainsFunc(c.Priority, func(w string) bool { return strings.EqualFold(w, l) })
	})
	j.Alone = len(f.Requested) == 1 && f.Requested[0] == c.Viewer
	j.Area = slices.ContainsFunc(f.Dirs, func(d string) bool { return c.Area[d] })

	switch {
	case j.Priority:
		j.say(statusPriority)
	case j.Area:
		j.say(statusArea)
	}
}

func authored(p *PullRequest, f *Facts, me string) (Lane, status) {
	if f.Decision == approved && !p.Draft && (f.Checks == "SUCCESS" || f.Checks == "") {
		if f.Checks == "SUCCESS" {
			return LaneReady, statusReady
		}

		return LaneReady, statusApproved
	}

	if s, ok := answered(f, me); ok {
		return LaneBack, s
	}

	return LaneWaiting, stalled(p, f)
}

// answered says what others did to your pull request after your last push.
func answered(f *Facts, me string) (status, bool) {
	if f.Pushed.IsZero() {
		return status{}, false
	}

	n := 0

	for _, r := range f.Reviews {
		if r.Author == me || !r.At.After(f.Pushed) {
			continue
		}

		if r.State == changesRequested {
			return statusAsked, true
		}

		n++
	}

	for _, t := range f.Talk {
		if t.Author != me && t.At.After(f.Pushed) {
			n++
		}
	}

	if n == 0 {
		return status{}, false
	}

	return comments(n), true
}

func stalled(p *PullRequest, f *Facts) status {
	switch {
	case p.Draft:
		return statusDraft
	case f.Decision == changesRequested:
		return statusAsked
	case f.Decision == approved && f.Checks == "FAILURE":
		return statusChecksFail
	case f.Decision == approved:
		return statusChecksWait
	}

	return statusAwaiting
}

func others(via Source, f *Facts, me string) (Lane, status) {
	mine, ok := latestBy(f.Reviews, me)

	switch {
	case !ok && via&Reviewed == 0:
		return LaneNew, status{}
	case !ok:
		return LaneWaiting, statusYouReviewed
	case !f.Pushed.IsZero() && f.Pushed.After(mine.At):
		return LaneBack, statusPushed
	}

	switch mine.State {
	case approved:
		return LaneWaiting, statusYouApproved
	case changesRequested:
		return LaneWaiting, statusYouAsked
	}

	return LaneWaiting, statusYouComment
}

func latestBy(events []Event, login string) (Event, bool) {
	i := slices.IndexFunc(events, func(e Event) bool { return e.Author == login })
	if i < 0 {
		return Event{}, false
	}

	return events[i], true
}

// who names the other humans a review is waiting on. Where you are one of them
// it reads from your side, as "only you" or "+ maria".
func who(requested []string, me string) string {
	others := slices.DeleteFunc(slices.Clone(requested), func(l string) bool { return l == me })

	if slices.Contains(requested, me) {
		switch len(others) {
		case 0:
			return "only you"
		case 1:
			return "+ " + others[0]
		}

		return fmt.Sprintf("+ %d", len(others))
	}

	switch len(others) {
	case 0:
		return ""
	case 1:
		return others[0]
	}

	return fmt.Sprintf("%s +%d", others[0], len(others)-1)
}

func feeds(l Lane) Source {
	switch l {
	case LaneNew:
		return Requested
	case LaneMerged:
		return Merged
	case LaneReady, LaneBack, LaneWaiting:
	}

	return Requested | Reviewed | Authored
}

// Lanes sorts what the searches found into the five lanes, each ranked by order.
// Each row's Judgement is recorded in known, which is what order reads.
//
// A lane is pending while any search feeding it is, and carries the first
// failure among them, so one search failing leaves the rest of the queue up.
func Lanes(searched []Bucket, facts map[string]Facts, c Context, known map[string]Known, order Order) []Bucket {
	rows, via := gather(searched)

	lanes := make([]Bucket, len(laneNames))
	for i := range lanes {
		lanes[i] = Bucket{Name: Lane(i).String(), done: true}
	}

	judged := make([]Lane, len(rows))
	for i := range rows {
		judged[i] = record(&rows[i], via[keyOf(&rows[i])], facts, c, known).Lane
	}

	into := stacked(rows, facts, known)

	for i := range rows {
		lane, ok := into[keyOf(&rows[i])]
		if !ok {
			lane = judged[i]
		}

		lanes[lane].Items = append(lanes[lane].Items, rows[i])
	}

	at := func(p *PullRequest) Known { return known[keyOf(p)] }

	for i := range lanes {
		Arrange(lanes[i].Items, at, order)

		for j := range searched {
			if feeds(Lane(i))&searched[j].via == 0 {
				continue
			}

			lanes[i].done = lanes[i].done && searched[j].done

			if lanes[i].Err == "" {
				lanes[i].Err = searched[j].Err
			}
		}
	}

	return lanes
}

// Annotate records a Judgement for rows no lane owns, so a configured section
// can still draw who a review waits on and why.
func Annotate(items []PullRequest, facts map[string]Facts, c Context, known map[string]Known) {
	for i := range items {
		record(&items[i], 0, facts, c, known)
	}
}

func record(p *PullRequest, via Source, facts map[string]Facts, c Context, known map[string]Known) Judgement {
	key := keyOf(p)

	var f *Facts

	if got, ok := facts[key]; ok {
		f = &got
	}

	j := Judge(p, via, f, c)

	k := known[key]
	k.Judgement, k.Stack = j, Stack{}
	known[key] = k

	return j
}

// MarkMerged moves a row to the merged search's bucket, which is where a pull
// request merged from the queue belongs.
func MarkMerged(searched []Bucket, key string) {
	var moved *PullRequest

	for i := range searched {
		if searched[i].via&Merged != 0 {
			continue
		}

		at := slices.IndexFunc(searched[i].Items, func(p PullRequest) bool { return keyOf(&p) == key })
		if at < 0 {
			continue
		}

		p := searched[i].Items[at]
		moved = &p
		searched[i].Items = slices.Delete(searched[i].Items, at, at+1)
	}

	if moved == nil {
		return
	}

	for i := range searched {
		if searched[i].via&Merged != 0 {
			searched[i].Items = append(searched[i].Items, *moved)
		}
	}
}

// gather folds the searches into one row per pull request, remembering which
// searches found it.
func gather(searched []Bucket) ([]PullRequest, map[string]Source) {
	var rows []PullRequest

	via := map[string]Source{}

	for i := range searched {
		for j := range searched[i].Items {
			p := &searched[i].Items[j]

			key := keyOf(p)
			if _, seen := via[key]; !seen {
				rows = append(rows, *p)
			}

			via[key] |= searched[i].via
		}
	}

	return rows, via
}

func keyOf(p *PullRequest) string { return fmt.Sprintf("%s#%d", p.Repository, p.Number) }

// Rows is every open row of the searches, which is what a facts read asks about.
func Rows(searched []Bucket) []PullRequest {
	var out []PullRequest

	for i := range searched {
		if searched[i].via&Merged == 0 {
			out = append(out, searched[i].Items...)
		}
	}

	return out
}
