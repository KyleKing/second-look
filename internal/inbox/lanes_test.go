package inbox_test

import (
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/inbox"
)

func TestJudge(t *testing.T) {
	t.Parallel()

	const me = "kyle"

	pushed := at("2026-09-01T12:00:00Z")
	before, after := pushed.Add(-time.Hour), pushed.Add(time.Hour)
	c := inbox.Context{Viewer: me, Priority: []string{"Urgent"}, Area: map[string]bool{"api": true}}

	mine := inbox.PullRequest{Author: me}
	theirs := inbox.PullRequest{Author: "maria", Labels: []string{"urgent"}}

	for _, tc := range []struct {
		name string
		pr   inbox.PullRequest
		via  inbox.Source
		f    *inbox.Facts
		want inbox.Judgement
	}{
		{
			name: "approved and green is ready", pr: mine, via: inbox.Authored,
			f:    &inbox.Facts{Decision: "APPROVED", Checks: "SUCCESS", Pushed: pushed},
			want: inbox.Judgement{Lane: inbox.LaneReady, Why: "approved, checks passing"},
		},
		{
			name: "approved with failing checks waits", pr: mine, via: inbox.Authored,
			f:    &inbox.Facts{Decision: "APPROVED", Checks: "FAILURE", Pushed: pushed},
			want: inbox.Judgement{Lane: inbox.LaneWaiting, Why: "checks failing"},
		},
		{
			name: "comments after the push come back", pr: mine, via: inbox.Authored,
			f: &inbox.Facts{
				Pushed: pushed, Requested: []string{"maria"},
				Talk: []inbox.Event{
					{Author: "maria", At: after}, {Author: "maria", At: before}, {Author: me, At: after},
				},
			},
			want: inbox.Judgement{Lane: inbox.LaneBack, Who: "maria", Why: "1 new comment"},
		},
		{
			name: "a push after your review comes back", pr: theirs, via: inbox.Reviewed,
			f:    &inbox.Facts{Pushed: pushed, Reviews: []inbox.Event{{Author: me, State: "COMMENTED", At: before}}},
			want: inbox.Judgement{Lane: inbox.LaneBack, Why: "pushed since your review"},
		},
		{
			name: "requested and unreviewed is new, priority first", pr: theirs, via: inbox.Requested,
			f:    &inbox.Facts{Requested: []string{me, "sam", "lee"}, Dirs: []string{"api"}},
			want: inbox.Judgement{Lane: inbox.LaneNew, Who: "+ 2", Why: "priority", Priority: true, Area: true},
		},
		{
			name: "only you is said and the area is the reason", pr: inbox.PullRequest{Author: "maria"},
			via:  inbox.Requested,
			f:    &inbox.Facts{Requested: []string{me}, Dirs: []string{"api"}},
			want: inbox.Judgement{Lane: inbox.LaneNew, Who: "only you", Why: "same area", Alone: true, Area: true},
		},
		{
			name: "no facts is provisional", pr: theirs, via: inbox.Requested,
			want: inbox.Judgement{Lane: inbox.LaneNew},
		},
		{
			name: "merged search is merged", pr: theirs, via: inbox.Merged,
			want: inbox.Judgement{Lane: inbox.LaneMerged},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := inbox.Judge(&tc.pr, tc.via, tc.f, c); got != tc.want {
				t.Errorf("Judge() is %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecodeFactsDropsBotsAndTeams(t *testing.T) {
	t.Parallel()

	body := `{"data":{"viewer":{"login":"kyle"},"nodes":[null,{"number":7,
	"repository":{"nameWithOwner":"o/r"},"reviewDecision":"APPROVED",
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

	if len(f.Dirs) != 2 || f.Dirs[0] != "api" || f.Dirs[1] != "web" || f.Checks != "" {
		t.Errorf("top-level directories and no checks: %+v", f)
	}
}
