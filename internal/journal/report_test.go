package journal_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/journal"
)

func feed(lines ...string) *journal.Summary {
	return journal.Summarize(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

func TestSummarizeCountsKeysEventsAndWaits(t *testing.T) {
	t.Parallel()

	s := feed(
		`{"t":"2026-10-11T09:00:00Z","s":"shell","e":"open"}`,
		`{"t":"2026-10-11T09:00:01Z","s":"review","e":"key","w":"A"}`,
		`{"t":"2026-10-11T09:00:02Z","s":"review","e":"key","w":"p"}`,
		`{"t":"2026-10-11T09:00:03Z","s":"review","e":"key","w":"g"}`,
		`{"t":"2026-10-11T09:00:04Z","s":"review","e":"key","w":"d"}`,
		`{"t":"2026-10-11T09:00:05Z","s":"queue","e":"key","w":"j"}`,
		`{"t":"2026-10-11T09:00:06Z","s":"review","e":"head","ms":250}`,
		`{"t":"2026-10-11T09:00:07Z","s":"review","e":"pane nvim"}`,
		`{"t":"2026-10-11T09:00:08Z","s":"review","e":"post"}`,
		`{"t":"2026-10-11T09:00:09Z","s":"shell","e":"leave","ms":192000}`,
		`not json at all`,
		`{"t":"2026-10-11T12:00:00Z","s":"shell","e":"open"}`,
	)

	if s.Lines != 11 {
		t.Errorf("counted %d lines, want 11 (the bad line skipped)", s.Lines)
	}

	if s.Sittings() != 2 {
		t.Errorf("counted %d sittings, want 2 (a 3-hour gap splits)", s.Sittings())
	}

	if s.Events["open"] != 2 || s.Events["post"] != 1 {
		t.Errorf("events counted %v", s.Events)
	}

	if s.Keys["review"]["A"] != 1 || s.Keys["queue"]["j"] != 1 {
		t.Errorf("keys counted %v", s.Keys)
	}

	if s.Follows["A"]["p"] != 1 || s.Follows["g"]["d"] != 1 {
		t.Errorf("chord followers counted %v", s.Follows)
	}

	if got := s.Waits["head"]; len(got) != 1 || got[0] != 250 {
		t.Errorf("the head wait read %v", got)
	}

	if _, ok := s.Follows["j"]; ok {
		t.Errorf("j is no leader, but it was followed: %v", s.Follows["j"])
	}
}

func TestWriteReportsWhatWasReachedFor(t *testing.T) {
	t.Parallel()

	s := feed(
		`{"t":"2026-10-11T09:00:00Z","s":"shell","e":"open"}`,
		`{"t":"2026-10-11T09:00:01Z","s":"review","e":"key","w":"A"}`,
		`{"t":"2026-10-11T09:00:02Z","s":"review","e":"key","w":"p"}`,
		`{"t":"2026-10-11T09:10:00Z","s":"review","e":"head","ms":250}`,
		`{"t":"2026-10-11T09:12:00Z","s":"shell","e":"leave","ms":192000}`,
	)

	var b bytes.Buffer
	if err := s.Write(&b); err != nil {
		t.Fatal(err)
	}

	got := b.String()
	for _, want := range []string{
		"5 events · 1 sitting",
		"reviews   1 opened · 0 posted · 0 merged",
		"head      n=1 median 250ms",
		"leave     n=1 median 3m12s",
		"review    A 1 · p 1",
		"after A   p 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report is missing %q:\n%s", want, got)
		}
	}
}

func TestWriteAnEmptyJournalSaysSo(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	if err := feed().Write(&b); err != nil {
		t.Fatal(err)
	}

	if b.String() != "no journal yet\n" {
		t.Errorf("an empty journal printed %q", b.String())
	}
}

// A sitting that never paused for half an hour is one sitting, however many
// entries it holds; the gap is what ends it.
func TestSittingsSplitOnQuiet(t *testing.T) {
	t.Parallel()

	minutes := []string{"09:00", "09:10", "09:20", "10:30", "10:40"}
	lines := make([]string, 0, len(minutes))

	for _, minute := range minutes {
		lines = append(lines,
			strings.ReplaceAll(`{"t":"2026-10-11TMM:00Z","s":"queue","e":"key","w":"j"}`, "MM", minute))
	}

	s := feed(lines...)
	if s.Sittings() != 2 {
		t.Fatalf("counted %d sittings, want 2", s.Sittings())
	}

	if len(s.SitLens) != 2 || s.SitLens[0] != 20*time.Minute || s.SitLens[1] != 10*time.Minute {
		t.Errorf("the sitting lengths read %v", s.SitLens)
	}
}
