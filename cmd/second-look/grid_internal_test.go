package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

func spanText(spans []tui.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}

	return b.String()
}

// A ref drops its owner unless another owner's repository shares the name, and
// a ref with no number is the repository alone.
func checkRefParts(t *testing.T) {
	t.Helper()

	shared := sharedNames([]place{{repo: "acme/api"}, {repo: "kyle/api"}, {repo: "acme/web"}})
	if !shared["api"] || shared["web"] {
		t.Fatalf("shared names are %v", shared)
	}

	for _, tc := range []struct {
		p           place
		owner, rest string
	}{
		{place{repo: "acme/web", number: 1}, "", "web#1"},
		{place{repo: "acme/web", number: 118}, "", "web#118"},
		{place{repo: "acme/web"}, "", "web"},
		{place{repo: "kyle/api", number: 2}, "kyle/", "api#2"},
	} {
		if owner, rest := tc.p.parts(shared); owner != tc.owner || rest != tc.rest {
			t.Errorf("%+v reads %q %q, want %q %q", tc.p, owner, rest, tc.owner, tc.rest)
		}
	}
}

// stagedAt is a review staged under a fresh checkout, with the pull request's
// title cached at its head when one is given.
func stagedAt(t *testing.T, title string) prepared.Review {
	t.Helper()

	root := t.TempDir()
	r := prepared.Review{
		Repository: "o/r", Number: 7, HeadSHA: "abc123",
		Path: filepath.Join(root, artifact.Dir, "pr-7.toml"),
	}

	if title != "" {
		if err := artifact.SaveAbout(root, r.HeadSHA, threads.About{Title: title}); err != nil {
			t.Fatal(err)
		}
	}

	return r
}

// A staged row is called what its pull request is called. One whose title was
// never cached says so with the placeholder and is named by its ref in the
// inspector, and one that cannot be read says why where its title would be.
func checkAStagedReviewIsCalledByItsTitle(t *testing.T) {
	t.Helper()

	titled := stagedAt(t, "fix(read): wrap the error")
	bare := stagedAt(t, "")
	broken := stagedAt(t, "fix(read): wrap the error")
	broken.Broken = "line 3: expected a key"

	s := &reviewsScreen{}
	now := time.Now()

	cases := []struct {
		name                 string
		r                    prepared.Review
		title, cell          string
		subject, prefix, ref string
	}{
		{"titled", titled, "fix(read): wrap the error", "  wrap the error", "wrap the error", "fix(read)", "o/r#7"},
		{"untitled", bare, "", "  ·", "o/r#7", "", ""},
		{
			"broken", broken, "fix(read): wrap the error", "  line 3: expected a key",
			"wrap the error", "fix(read)", "o/r#7",
		},
	}

	for i := range cases {
		tc := &cases[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := title(&tc.r); got != tc.title {
				t.Errorf("title is %q, want %q", got, tc.title)
			}

			if got := spanText(titleCell(&tc.r).Spans); got != tc.cell {
				t.Errorf("the title cell reads %q, want %q", got, tc.cell)
			}

			about := s.reviewAbout(&tc.r, now)
			if about.Subject != tc.subject || about.Prefix != tc.prefix || about.Ref != tc.ref {
				t.Errorf("the inspector says %q %q %q, want %q %q %q",
					about.Subject, about.Prefix, about.Ref, tc.subject, tc.prefix, tc.ref)
			}

			if about.Facts[0].Text != tc.r.Broken {
				t.Errorf("the first fact is %q, want the reason it cannot be read", about.Facts[0].Text)
			}
		})
	}
}

// A word a row may not have draws nothing, and one it has draws beside its glyph.
func checkWordCell(t *testing.T) {
	t.Helper()

	if got := wordCell("✓", "", tui.RoleCell); len(got.Spans) != 0 || len(got.Long) != 0 {
		t.Errorf("no word drew %+v", got)
	}

	got := wordCell("✓", "merged", tui.RoleCell)
	if spanText(got.Spans) != "✓ merged" || got.Spans[0].Role != tui.RoleCell {
		t.Errorf("a word drew %+v", got)
	}
}

// A row the reader has opened says so in its status, on the short words and,
// where the status carries them, the long ones, and nowhere it has none.
func checkAnOpenedRowSaysSo(t *testing.T) {
	t.Helper()

	p := inbox.PullRequest{Repository: "o/r", Number: 2, Title: "fix: a thing", Author: "kyle"}
	key := keyOf(&p)
	s := &inboxScreen{facts: map[string]inbox.Facts{key: {}}, seen: map[string]bool{key: true}}

	quiet := s.cells(&p, inbox.Known{}, nil, time.Now())[colStatus]
	if spanText(quiet.Spans) != " · opened" || quiet.Long != nil {
		t.Errorf("a row with no status of its own drew %+v", quiet)
	}

	k := inbox.Known{Judgement: inbox.Judgement{Glyph: "↩", Short: "back", Long: "back to you"}}

	said := s.cells(&p, k, nil, time.Now())[colStatus]
	if spanText(said.Spans) != "↩ back · opened" || spanText(said.Long) != "↩ back to you · opened" {
		t.Errorf("a row with a status drew %q and %q", spanText(said.Spans), spanText(said.Long))
	}

	delete(s.seen, key)

	if unseen := s.cells(&p, k, nil, time.Now())[colStatus]; spanText(unseen.Spans) != "↩ back" {
		t.Errorf("a row never opened drew %q", spanText(unseen.Spans))
	}
}

// The header says what the queue is still waiting on, one clause at a time.
func checkTheQueueStateSaysWhatIsStillOut(t *testing.T) {
	t.Helper()

	cases := []struct {
		name string
		s    inboxScreen
		want string
		busy bool
	}{
		{"settled", inboxScreen{}, "", false},
		{"searching", inboxScreen{waiting: 2}, "2 searches still out", true},
		{
			"searching a snapshot",
			inboxScreen{waiting: 1, age: 3 * time.Hour},
			"cached 3h ago · 1 search still out", true,
		},
		{"rating", inboxScreen{pending: 3}, "3 rows still being rated", true},
		{"unrated", inboxScreen{unread: 1, ready: 2}, "1 row could not be rated · 2 staged ahead", false},
		{"reading reviews", inboxScreen{factsOut: true}, "· reading reviews…", true},
		{
			"reading reviews while staging",
			inboxScreen{factsOut: true, fetching: 1},
			"staging 1 ahead · reading reviews…", true,
		},
		{
			"provisional with a failed search",
			inboxScreen{ready: 1, factsErr: "offline", buckets: []inbox.Bucket{{Err: "403"}, {}}},
			"1 staged ahead · lanes provisional · 1 search failed", false,
		},
	}

	for i := range cases {
		tc := &cases[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.s.state(); got.Text != tc.want || got.Busy != tc.busy {
				t.Errorf("state is %+v, want %q busy=%v", got, tc.want, tc.busy)
			}
		})
	}
}

// A configured queue keeps the sections a person wrote, and still judges each
// row so its status and lane read the same as the default queue's.
func checkAConfiguredQueueStillJudgesItsRows(t *testing.T) {
	t.Helper()

	items := []inbox.PullRequest{
		{Repository: "o/r", Number: 1, Author: "someone", Draft: true},
		{Repository: "o/r", Number: 2, Author: "someone"},
	}

	s := &inboxScreen{
		configured: true, viewer: "me",
		buckets: []inbox.Bucket{{Name: "mine", Items: items}},
		facts:   map[string]inbox.Facts{}, local: map[string]inbox.Known{},
	}
	s.regroup()

	if len(s.lanes) != 1 || s.lanes[0].Name != "mine" {
		t.Fatalf("the lanes are %+v, want the configured section", s.lanes)
	}

	keys := make([]string, 0, len(s.local))
	for key := range s.local {
		if s.local[key].Short == "" && s.local[key].Lane == 0 && s.local[key].Why == "" {
			t.Errorf("%s was never judged", key)
		}

		keys = append(keys, key)
	}

	slices.Sort(keys)

	if want := []string{"o/r#1", "o/r#2"}; !slices.Equal(keys, want) {
		t.Errorf("judged %v, want %v", keys, want)
	}
}

// TestGridCells runs each rule of what a queue's row draws in its cells and its
// header, against the screens' own types.
func TestGridCells(t *testing.T) {
	t.Parallel()

	for name, check := range map[string]func(*testing.T){
		"RefParts":                           checkRefParts,
		"AStagedReviewIsCalledByItsTitle":    checkAStagedReviewIsCalledByItsTitle,
		"WordCell":                           checkWordCell,
		"AnOpenedRowSaysSo":                  checkAnOpenedRowSaysSo,
		"TheQueueStateSaysWhatIsStillOut":    checkTheQueueStateSaysWhatIsStillOut,
		"AConfiguredQueueStillJudgesItsRows": checkAConfiguredQueueStillJudgesItsRows,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t)
		})
	}
}
