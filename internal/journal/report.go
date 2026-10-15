package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// SittingGap is the quiet that ends one sitting and starts the next: longer
// than any pause inside a review, shorter than the pause between two.
const SittingGap = 30 * time.Minute

// leaders are the keys that open a chord — Model.startsChord's set — so the
// summary can say what each leader was followed by, which is the question the
// A chord and the renderer keys are parked on. A leader never pressed is not
// reported.
var leaders = map[string]bool{
	"u": true, "g": true, "z": true, "m": true, ",": true,
	"A": true, "]": true, "[": true, "S": true, "H": true, "a": true,
}

// waits are the events a Since call writes: their ms is the measurement, not
// an attribute. Everything else with an E is counted.
var waits = map[string]bool{"head": true, "leave": true}

// wrapAt is where a ranked row breaks onto a continuation line, and
// rowIndent pads the label column and its continuation alike.
const (
	wrapAt    = 78
	rowIndent = 10
)

// secsMin is the carry between a duration's spelled parts.
const secsMin = 60

// Summary is the whole journal read into the shapes the parked decisions ask
// about: what was reached for, how often, and how long each wait ran.
type Summary struct {
	Lines   int
	First   time.Time
	Last    time.Time
	SitLens []time.Duration

	Keys    map[string]map[string]int // screen -> key -> count
	Events  map[string]int            // event -> count, "pane x" keeping its name
	Waits   map[string][]int64        // event -> ms samples
	Follows map[string]map[string]int // leader -> next key -> count
}

// Sittings is how many sittings the journal covers.
func (s *Summary) Sittings() int { return len(s.SitLens) }

// Summarize reads every line of r, skipping the ones it cannot parse: the
// journal is best-effort on the way in and stays that way on the way out.
func Summarize(r io.Reader) *Summary {
	s := &Summary{
		Keys:    map[string]map[string]int{},
		Events:  map[string]int{},
		Waits:   map[string][]int64{},
		Follows: map[string]map[string]int{},
	}

	var at, sitStart time.Time
	lastKey := ""

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}

		s.Lines++

		if t, err := time.Parse(time.RFC3339, e.T); err == nil {
			if !s.First.IsZero() && t.Sub(at) > SittingGap {
				s.SitLens = append(s.SitLens, at.Sub(sitStart))
				sitStart = t
			}

			if s.First.IsZero() {
				s.First, sitStart = t, t
			}

			at, s.Last = t, t
		}

		lastKey = s.add(e, lastKey)
	}

	if !at.IsZero() {
		s.SitLens = append(s.SitLens, at.Sub(sitStart))
	}

	return s
}

// add folds one entry in and answers the key the next one may have followed:
// a chord's second key counts against the leader that waited on it, and only
// on the review screen, where the chords live.
func (s *Summary) add(e entry, lastKey string) string {
	switch {
	case e.E == "key":
		if s.Keys[e.S] == nil {
			s.Keys[e.S] = map[string]int{}
		}

		s.Keys[e.S][e.W]++

		if leaders[lastKey] && e.S == "review" {
			if s.Follows[lastKey] == nil {
				s.Follows[lastKey] = map[string]int{}
			}

			s.Follows[lastKey][e.W]++
		}

		return e.W
	case waits[e.E] || e.MS > 0:
		s.Waits[e.E] = append(s.Waits[e.E], e.MS)
	default:
		s.Events[e.E]++
	}

	return ""
}

// Write draws the summary the way status does: one line per thing, `·`
// between the parts, and nothing styled, since it answers in a pipe.
func (s *Summary) Write(w io.Writer) error {
	if s.Lines == 0 {
		if _, err := fmt.Fprintln(w, "no journal yet"); err != nil {
			return fmt.Errorf("writing the report: %w", err)
		}

		return nil
	}

	var b strings.Builder

	fmt.Fprintf(&b, "%d events · %s in %s\n",
		s.Lines, plural(s.Sittings(), "sitting"), s.First.Format("Jan 2"))

	if len(s.SitLens) > 0 {
		fmt.Fprintf(&b, "sittings  median %s · longest %s\n",
			dur(medianDur(s.SitLens)), dur(slices.Max(s.SitLens)))
	}

	if s.Events["open"]+s.Events["post"]+s.Events["merge"] > 0 {
		fmt.Fprintf(&b, "reviews   %d opened · %d posted · %d merged\n",
			s.Events["open"], s.Events["post"], s.Events["merge"])
	}

	for _, ev := range sortedKeys(s.Waits) {
		ms := s.Waits[ev]
		fmt.Fprintf(&b, "%-9s n=%d median %s · worst %s\n",
			ev, len(ms), dur(time.Duration(medianInt(ms))*time.Millisecond),
			dur(time.Duration(slices.Max(ms))*time.Millisecond))
	}

	for _, scr := range sortedKeys(s.Keys) {
		writeRanked(&b, scr, s.Keys[scr])
	}

	for _, lead := range sortedKeys(s.Follows) {
		writeRanked(&b, "after "+lead, s.Follows[lead])
	}

	writeRanked(&b, "panes", prefixed(s.Events, "pane "))
	writeRanked(&b, "agents", prefixed(s.Events, "agent "))

	if _, err := fmt.Fprint(w, b.String()); err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}

	return nil
}

// writeRanked draws one "name · name" row of counts, wrapping under its label
// when the line runs long rather than truncating any key away.
func writeRanked(b *strings.Builder, name string, counts map[string]int) {
	if len(counts) == 0 {
		return
	}

	type pair struct {
		k string
		n int
	}

	pairs := make([]pair, 0, len(counts))
	for k, n := range counts {
		pairs = append(pairs, pair{k, n})
	}

	slices.SortFunc(pairs, func(a, b pair) int {
		if a.n != b.n {
			return b.n - a.n
		}

		return strings.Compare(a.k, b.k)
	})

	line := fmt.Sprintf("%-*s ", rowIndent-1, name)
	for _, p := range pairs {
		if len(line) > wrapAt {
			fmt.Fprintln(b, line)
			line = strings.Repeat(" ", rowIndent)
		}

		line += fmt.Sprintf("%s %d · ", p.k, p.n)
	}

	fmt.Fprintln(b, strings.TrimSuffix(line, " · "))
}

// prefixed answers the events carrying a tenant or an agent name, keyed by
// the name alone: "pane nvim" counts under nvim.
func prefixed(events map[string]int, prefix string) map[string]int {
	out := map[string]int{}

	for e, n := range events {
		if name, ok := strings.CutPrefix(e, prefix); ok {
			out[name] += n
		}
	}

	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

func medianInt(ms []int64) int64 {
	sorted := slices.Clone(ms)
	slices.Sort(sorted)

	return sorted[len(sorted)/2]
}

func medianDur(ds []time.Duration) time.Duration {
	sorted := slices.Clone(ds)
	slices.Sort(sorted)

	return sorted[len(sorted)/2]
}

// dur spells a duration the way the journal's reader thinks about it:
// milliseconds for a wait, minutes for a sitting, hours past that.
func dur(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%secsMin)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%secsMin)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	return fmt.Sprintf("%d %ss", n, word)
}
