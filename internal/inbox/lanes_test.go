package inbox_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kyleking/second-look/internal/inbox"
)

type judgeCase struct {
	name string
	pr   inbox.PullRequest
	via  inbox.Source
	f    *inbox.Facts
	want inbox.Judgement
}

func judgeAll(t *testing.T, c inbox.Context, cases []judgeCase) {
	t.Helper()

	for i := range cases {
		tc := &cases[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := inbox.Judge(&tc.pr, tc.via, tc.f, c)
			if got != tc.want {
				t.Errorf("Judge() is %+v, want %+v", got, tc.want)
			}

			if n := utf8.RuneCountInString(got.Short); n > inbox.ShortWidth {
				t.Errorf("short status %q is %d cells, over the %d a narrow cell has", got.Short, n, inbox.ShortWidth)
			}
		})
	}
}

func TestJudge(t *testing.T) {
	t.Parallel()

	const me = "kyle"

	pushed := at("2026-09-01T12:00:00Z")
	before, after := pushed.Add(-time.Hour), pushed.Add(time.Hour)
	c := inbox.Context{Viewer: me, Priority: []string{"Urgent"}, Area: map[string]bool{"api": true}}

	mine := inbox.PullRequest{Author: me}
	theirs := inbox.PullRequest{Author: "maria", Labels: []string{"urgent"}}

	judgeAll(t, c, []judgeCase{
		{
			name: "approved and green is ready", pr: mine, via: inbox.Authored,
			f: &inbox.Facts{Decision: "APPROVED", Checks: "SUCCESS", Pushed: pushed},
			want: inbox.Judgement{
				Lane: inbox.LaneReady, Why: "approved, checks passing",
				Glyph: "✓", Short: "approved", Long: "approved, checks passing", Tone: inbox.ToneGood,
			},
		},
		{
			name: "approved with failing checks waits", pr: mine, via: inbox.Authored,
			f: &inbox.Facts{Decision: "APPROVED", Checks: "FAILURE", Pushed: pushed},
			want: inbox.Judgement{
				Lane: inbox.LaneWaiting, Why: "checks failing",
				Glyph: "✗", Short: "checks failing", Long: "checks failing", Tone: inbox.ToneBad,
			},
		},
		{
			name: "comments after the push come back", pr: mine, via: inbox.Authored,
			f: &inbox.Facts{
				Pushed: pushed, Requested: []string{"maria"},
				Talk: []inbox.Event{
					{Author: "maria", At: after}, {Author: "maria", At: before}, {Author: me, At: after},
				},
			},
			want: inbox.Judgement{
				Lane: inbox.LaneBack, Who: "maria", Why: "1 new comment",
				Glyph: "◆", Short: "1 comment", Long: "1 new comment", Tone: inbox.ToneWarn,
			},
		},
		{
			name: "merged search is merged", pr: theirs, via: inbox.Merged,
			want: inbox.Judgement{
				Lane: inbox.LaneMerged, Why: "merged",
				Glyph: "✓", Short: "merged", Long: "merged", Tone: inbox.ToneMerged,
			},
		},
	})
}

// The to-review lane is one ask with several shapes: a first ask, a re-ask,
// a push that reopens a review you already gave, and asks that name the
// crowd around you rather than the ask itself.
func TestJudgeToReview(t *testing.T) {
	t.Parallel()

	const me = "kyle"

	pushed := at("2026-09-01T12:00:00Z")
	before, after := pushed.Add(-time.Hour), pushed.Add(time.Hour)
	c := inbox.Context{Viewer: me, Priority: []string{"Urgent"}, Area: map[string]bool{"api": true}}

	theirs := inbox.PullRequest{Author: "maria", Labels: []string{"urgent"}}

	judgeAll(t, c, []judgeCase{
		{
			name: "a push after your review is yours to review again", pr: theirs, via: inbox.Reviewed,
			f: &inbox.Facts{Pushed: pushed, Reviews: []inbox.Event{{Author: me, State: "COMMENTED", At: before}}},
			want: inbox.Judgement{
				Lane: inbox.LaneReview, Why: "pushed since your review", Priority: true,
				Glyph: "↻", Short: "new commits", Long: "pushed since your review", Tone: inbox.ToneNew,
			},
		},
		{
			name: "a review still asked after you commented is yours", pr: theirs, via: inbox.Requested,
			f: &inbox.Facts{
				Pushed: pushed, Requested: []string{me, "sam"},
				Reviews: []inbox.Event{{Author: me, State: "COMMENTED", At: after}},
			},
			want: inbox.Judgement{
				Lane: inbox.LaneReview, Who: "+ sam", Why: "asked for your review again", Priority: true,
				Glyph: "↻", Short: "re-asked", Long: "asked for your review again", Tone: inbox.ToneNew,
			},
		},
		{
			name: "requested and unreviewed is yours, shared with others", pr: theirs, via: inbox.Requested,
			f: &inbox.Facts{Requested: []string{me, "sam", "lee"}, Dirs: []string{"api"}},
			want: inbox.Judgement{
				Lane: inbox.LaneReview, Who: "+ 2", Why: "asked of you and 2 others", Priority: true, Area: true,
				Glyph: "○", Short: "+ 2", Long: "asked of you and 2 others", Tone: inbox.ToneNew,
			},
		},
		{
			name: "a team's ask is said as one", pr: theirs, via: inbox.Requested,
			f: &inbox.Facts{Requested: []string{"maria"}},
			want: inbox.Judgement{
				Lane: inbox.LaneReview, Who: "maria", Why: "asked of your team", Priority: true,
				Glyph: "○", Short: "your team", Long: "asked of your team", Tone: inbox.ToneMuted,
			},
		},
		{
			name: "only you is said", pr: inbox.PullRequest{Author: "maria"},
			via: inbox.Requested,
			f:   &inbox.Facts{Requested: []string{me}, Dirs: []string{"api"}},
			want: inbox.Judgement{
				Lane: inbox.LaneReview, Who: "only you", Why: "asked of you alone", Alone: true, Area: true,
				Glyph: "◆", Short: "only you", Long: "asked of you alone", Tone: inbox.ToneWarn,
			},
		},
		{
			name: "no facts is provisional", pr: theirs, via: inbox.Requested,
			want: inbox.Judgement{Lane: inbox.LaneReview},
		},
	})
}

func TestDecodeFactsDropsBotsAndTeams(t *testing.T) {
	t.Parallel()

	body := `{"data":{"viewer":{"login":"kyle"},"nodes":[null,{"number":7,
	"repository":{"nameWithOwner":"o/r"},"baseRefName":"main","headRefName":"feat","headRefOid":"abc123",
	"reviewDecision":"APPROVED",
	"reviewRequests":{"nodes":[{"requestedReviewer":null},
	  {"requestedReviewer":{"__typename":"User","login":"maria"}},
	  {"requestedReviewer":{"__typename":"User","login":"ci[bot]"}}]},
	"latestReviews":{"nodes":[{"author":{"__typename":"Bot","login":"copilot"},"state":"COMMENTED",
	  "submittedAt":"2026-09-01T00:00:00Z"}]},
	"comments":{"nodes":[]},
	"commits":{"nodes":[{"commit":{"committedDate":"2026-09-01T00:00:00Z","statusCheckRollup":null}}]},
	"files":{"nodes":[{"path":"README.md"},{"path":"api/a.go"},{"path":"api/b.go"},{"path":"web/x.ts"}]}}]}}`

	got, err := inbox.DecodeFacts([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	f := got.Facts["o/r#7"]
	if got.Viewer != "kyle" || len(f.Requested) != 1 || f.Requested[0] != "maria" || len(f.Reviews) != 0 {
		t.Errorf("humans only: viewer %q, facts %+v", got.Viewer, f)
	}

	if f.Base != "main" || f.Head != "feat" || f.Oid != "abc123" {
		t.Errorf("the branches and the head commit are read: %+v", f)
	}

	if len(f.Dirs) != 2 || f.Dirs[0] != "api" || f.Dirs[1] != "web" || f.Checks != "" {
		t.Errorf("top-level directories and no checks: %+v", f)
	}
}

// A stack is one unit placed in the lane of its most urgent member, bottom first,
// and a row standing alone is ranked as before.
func TestLanesDrawAStackWhereItsMostUrgentMemberIs(t *testing.T) {
	t.Parallel()

	const me = "kyle"

	approved := inbox.Facts{Decision: "APPROVED", Checks: "SUCCESS", Base: "feat-a", Head: "feat-b"}
	searched := inbox.Plan(30)
	searched[0].Items = []inbox.PullRequest{
		{Repository: "o/r", Number: 12, Author: "sam", Created: at("2026-08-01T00:00:00Z")},
		{Repository: "o/r", Number: 10, Author: "maria", Created: at("2026-08-02T00:00:00Z")},
	}
	searched[2].Items = []inbox.PullRequest{{Repository: "o/r", Number: 11, Author: me}}

	facts := map[string]inbox.Facts{
		"o/r#10": {Requested: []string{me}, Base: "main", Head: "feat-a"},
		"o/r#11": approved,
		"o/r#12": {Requested: []string{me}, Base: "main", Head: "other"},
	}
	known := map[string]inbox.Known{}

	lanes := inbox.Lanes(searched, facts, inbox.Context{Viewer: me}, known, inbox.Rank)

	if got := numbers(lanes[inbox.LaneReady].Items); got != "10 11" {
		t.Errorf("ready lane holds %q, want the stack bottom first", got)
	}

	if got := numbers(lanes[inbox.LaneReview].Items); got != "12" {
		t.Errorf("to review lane holds %q, want the row standing alone", got)
	}

	bottom, top := known["o/r#10"], known["o/r#11"]
	if bottom.Lane != inbox.LaneReview || bottom.Stack != (inbox.Stack{ID: "o/r#10", Position: 1, Size: 2}) {
		t.Errorf("bottom keeps its own lane and joins the stack: %+v", bottom)
	}

	if !top.Stack.Above() || top.Stack.ID != "o/r#10" {
		t.Errorf("top is read after the bottom: %+v", top.Stack)
	}
}

func numbers(items []inbox.PullRequest) string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, strconv.Itoa(items[i].Number))
	}

	return strings.Join(out, " ")
}
