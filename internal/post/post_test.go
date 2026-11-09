package post_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/post"
)

var errBoom = errors.New("boom")

// fakePoster answers the GraphQL calls the post makes by which mutation the
// request carries, the way gh api graphql --input - would.
type fakePoster struct {
	endpoints  []string
	bodies     [][]byte
	failOn     int
	failDel    bool
	replyState string
}

func (f *fakePoster) Resolve(_ context.Context, threadID string) error {
	f.endpoints = append(f.endpoints, "resolve "+threadID)

	if f.failOn > 0 && len(f.endpoints) == f.failOn {
		return errBoom
	}

	return nil
}

func (f *fakePoster) Delete(_ context.Context, endpoint string) error {
	f.endpoints = append(f.endpoints, "delete "+endpoint)

	if f.failDel {
		return errBoom
	}

	return nil
}

func (f *fakePoster) Post(_ context.Context, endpoint string, body []byte) ([]byte, error) {
	f.endpoints = append(f.endpoints, endpoint)
	f.bodies = append(f.bodies, body)

	if f.failOn > 0 && len(f.endpoints) == f.failOn {
		return nil, errBoom
	}

	return []byte(`{"id":88,"node_id":"C_88"}`), nil
}

func (f *fakePoster) Upload(
	_ context.Context, _ string, _ int64, name, _ string, _ []byte,
) (string, error) {
	f.endpoints = append(f.endpoints, "upload "+name)

	if f.failOn > 0 && len(f.endpoints) == f.failOn {
		return "", errBoom
	}

	return "https://github.com/user-attachments/assets/fake-" + name, nil
}

func (f *fakePoster) GraphQL(_ context.Context, req []byte) ([]byte, error) {
	f.bodies = append(f.bodies, req)

	var call struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(req, &call); err != nil {
		return nil, fmt.Errorf("bad request: %w", err)
	}

	var name, answer string

	switch {
	case strings.Contains(call.Query, "addPullRequestReviewThreadReply"):
		state := f.replyState
		if state == "" {
			state = "COMMENTED"
		}
		name, answer = "graphql addReply",
			`{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_777","databaseId":777,`+
				`"pullRequestReview":{"databaseId":888,"state":"`+state+`"}}}}}`
	case strings.Contains(call.Query, "deletePullRequestReviewComment"):
		name, answer = "graphql deleteComment",
			`{"data":{"deletePullRequestReviewComment":{"pullRequestReviewComment":{"id":"PRRC_777"}}}}`
	case strings.Contains(call.Query, "addPullRequestReview"):
		name, answer = "graphql addReview",
			`{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_pending","databaseId":555}}}}`
	case strings.Contains(call.Query, "submitPullRequestReview"):
		name, answer = "graphql submit",
			`{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"PRR_pending","databaseId":555}}}}`
	case strings.Contains(call.Query, "reviewThreads"):
		name, answer = "graphql threads",
			`{"data":{"repository":{"pullRequest":{"reviewThreads":{
				"pageInfo":{"hasNextPage":false,"endCursor":""},
				"nodes":[{"id":"PRRT_found","comments":{"nodes":[{"databaseId":44}]}}]
			}}}}}`
	case strings.Contains(call.Query, "pullRequest(number"):
		name, answer = "graphql pullRequest",
			`{"data":{"repository":{"pullRequest":{"id":"PR_42"}}}}`
	case strings.Contains(call.Query, "databaseId"):
		name, answer = "graphql repoID",
			`{"data":{"repository":{"databaseId":9876}}}`
	default:
		return nil, fmt.Errorf("%w: %s", errUnmatched, call.Query)
	}

	f.endpoints = append(f.endpoints, name)

	if f.failOn > 0 && len(f.endpoints) == f.failOn {
		return nil, errBoom
	}

	return []byte(answer), nil
}

func inlineComment(id, status string) artifact.Comment {
	return artifact.Comment{ID: id, Path: "a.go", Line: 5, Side: artifact.SideRight, Body: "fix this", Status: status}
}

func review(comments ...artifact.Comment) *artifact.Review {
	return &artifact.Review{
		Version:  artifact.SchemaVersion,
		Owner:    "kyleking",
		Repo:     "second-look",
		Number:   42,
		HeadSHA:  "abc123",
		Event:    artifact.EventComment,
		Body:     "looks good",
		Comments: comments,
	}
}

// writeArtifact stages the review where artifact.Path puts it, so the posted
// record and the sweep land inside the test's own root.
func writeArtifact(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), artifact.Dir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("making the artifact dir: %v", err)
	}

	path := filepath.Join(dir, "pr-42.toml")
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("writing artifact: %v", err)
	}

	return path
}

var errUnmatched = errors.New("unmatched query")

func TestRun(t *testing.T) {
	t.Parallel()

	for _, tt := range runCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeArtifact(t)
			p := &fakePoster{failOn: tt.failOn, failDel: tt.failDel, replyState: tt.replyState}
			var out bytes.Buffer

			err := post.Run(context.Background(), p, path, tt.review, &out)

			checkErr(t, err, tt.wantErr)
			checkEndpoints(t, p.endpoints, tt.wantEndpoints)
			checkArtifact(t, path, tt.wantFileGone)
			checkRecord(t, path, tt.wantRecord)
			checkStaged(t, path, tt.wantStaged)
		})
	}
}

type runCase struct {
	name          string
	review        *artifact.Review
	failOn        int
	failDel       bool
	replyState    string
	wantErr       string
	wantEndpoints []string
	wantFileGone  bool
	wantRecord    string
	wantStaged    []string
}

func runCases() []runCase {
	return append(append(sentCases(), postSubmitCases()...), blockedCases()...)
}

func sentCases() []runCase {
	reply := artifact.Comment{
		ID: "2", InReplyTo: 99, Body: "thanks", Status: artifact.StatusReady, ThreadID: "PRRT_8",
	}

	wontFix := artifact.Comment{
		ID: "3", InReplyTo: 77, Body: "won't fix", Status: artifact.StatusReady,
		Resolve: true, ThreadID: "PRRT_9",
	}

	return []runCase{
		{
			name:   "happy path submits the review, then posts the reply on its own thread",
			review: review(inlineComment("1", artifact.StatusReady), reply),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit", "graphql addReply",
			},
			wantFileGone: true,
			wantRecord:   "pr-42-555.toml",
		},
		{
			name:    "a submit failure abandons the pending review rather than leaving it invisible",
			review:  review(inlineComment("1", artifact.StatusReady), reply),
			failOn:  3,
			wantErr: "the pending review it left was deleted",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit",
				"delete /repos/kyleking/second-look/pulls/42/reviews/555",
			},
		},
		{
			name:    "a pending review that cannot be deleted is named for a human",
			review:  review(inlineComment("1", artifact.StatusReady), reply),
			failOn:  3,
			failDel: true,
			wantErr: "close it on GitHub before posting again",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit",
				"delete /repos/kyleking/second-look/pulls/42/reviews/555",
			},
		},
		{
			name:   "a resolving reply resolves its thread once the reply lands",
			review: review(reply, wontFix),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit",
				"graphql addReply", "graphql addReply",
				"resolve PRRT_9",
			},
			wantFileGone: true,
			wantRecord:   "pr-42-555.toml",
		},
		{
			name:    "a thread that will not resolve still leaves the posted record",
			review:  review(wontFix),
			failOn:  5,
			wantErr: "the review posted as 555; the reply posted but its thread did not resolve",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit", "graphql addReply",
				"resolve PRRT_9",
			},
			wantFileGone: true,
			wantRecord:   "pr-42-555.toml",
		},
		{
			name: "a reply without a thread id looks the thread up by the comment it answers",
			review: review(inlineComment("1", artifact.StatusReady),
				artifact.Comment{ID: "6", InReplyTo: 44, Body: "on it", Status: artifact.StatusReady}),
			wantEndpoints: []string{
				"graphql threads", "graphql pullRequest", "graphql addReview",
				"graphql submit", "graphql addReply",
			},
			wantFileGone: true,
			wantRecord:   "pr-42-555.toml",
		},
		{
			name:    "a reply answering nothing on the pull request is refused before anything sends",
			review:  review(artifact.Comment{ID: "7", InReplyTo: 9999, Body: "huh", Status: artifact.StatusReady}),
			wantErr: "not a thread on this pull request",
			wantEndpoints: []string{
				"graphql threads",
			},
		},
		{
			name: "a todo reply posts like any reply",
			review: review(artifact.Comment{
				ID: "5", InReplyTo: 11, Body: "will fix",
				Status: artifact.StatusTodo, ThreadID: "PRRT_2",
			}),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit", "graphql addReply",
			},
			wantFileGone: true,
			wantRecord:   "pr-42-555.toml",
		},
	}
}

// postSubmitCases are the shapes the standalone replies add: a review holding
// nothing but replies sends no review at all, and a pending review the author
// has open elsewhere can still swallow one.
func postSubmitCases() []runCase {
	reply := artifact.Comment{
		ID: "2", InReplyTo: 99, Body: "thanks", Status: artifact.StatusReady, ThreadID: "PRRT_8",
	}

	return []runCase{
		{
			name:    "a reply that fails after the submit keeps only what did not go out",
			review:  review(inlineComment("1", artifact.StatusReady), reply),
			failOn:  4,
			wantErr: "the review posted as 555; reply 2: adding a reply: boom",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit", "graphql addReply",
			},
			wantRecord: "pr-42-555.toml",
			wantStaged: []string{"2"},
		},
		{
			name:   "a review holding only replies submits nothing — each posts on its own",
			review: reviewWithoutBody(reply),
			wantEndpoints: []string{
				"graphql addReply",
			},
			wantFileGone: true,
			wantRecord:   "pr-42-777.toml",
		},
		{
			name:       "a reply captured by a pending review is deleted back out and reported",
			review:     review(inlineComment("1", artifact.StatusReady), reply),
			replyState: "PENDING",
			wantErr:    "a pending review on this pull request captured the reply",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql submit",
				"graphql addReply", "graphql deleteComment",
			},
			wantRecord: "pr-42-555.toml",
			wantStaged: []string{"2"},
		},
	}
}

// reviewWithoutBody is review with no body or verdict, which is what a staged
// file holds after the review's own part already posted.
func reviewWithoutBody(comments ...artifact.Comment) *artifact.Review {
	r := review(comments...)
	r.Body = ""

	return r
}

func blockedCases() []runCase {
	return []runCase{
		{
			name:    "a todo comment of the review's own blocks the post and nothing is sent",
			review:  review(inlineComment("1", artifact.StatusTodo)),
			wantErr: "still todo",
		},
		{
			name:    "a reply with no text blocks the post and nothing is sent",
			review:  review(artifact.Comment{ID: "4", InReplyTo: 5, Status: artifact.StatusReady}),
			wantErr: "a reply with no text",
		},
		{
			name:    "a draft comment blocks the post and nothing is sent",
			review:  review(inlineComment("1", artifact.StatusDraft)),
			wantErr: "still draft",
		},
	}
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()

	if want == "" {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		return
	}

	if err == nil {
		t.Fatal("Run: want error, got nil")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func checkEndpoints(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("endpoints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("endpoints[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func checkArtifact(t *testing.T, path string, wantGone bool) {
	t.Helper()

	_, err := os.Stat(path)
	switch {
	case wantGone && !os.IsNotExist(err):
		t.Fatalf("artifact file still exists after a successful post: err = %v", err)
	case !wantGone && err != nil:
		t.Fatalf("artifact file missing when it should remain: %v", err)
	}
}

// checkRecord asserts the posted record exists under posted/, named for the
// id what posted landed as — the review, or the comment a lone reply became.
// An empty want asserts nothing, since the blocked cases post nothing and the
// abandoned ones leave no record either.
func checkRecord(t *testing.T, path, want string) {
	t.Helper()

	posted := filepath.Join(filepath.Dir(path), "posted")

	if want == "" {
		entries, err := os.ReadDir(posted)
		if err == nil && len(entries) > 0 {
			t.Fatalf("posted records exist after a post that sent nothing: %v", entries)
		}

		return
	}

	info, err := os.Stat(filepath.Join(posted, want))
	switch {
	case err != nil:
		t.Fatalf("no posted record at %s: %v", want, err)
	case info.Size() == 0:
		t.Fatalf("the posted record at %s is empty", want)
	}
}

// checkStaged loads the staged file the post left behind and names the
// comments it should still hold — nil when the file is expected to be gone or
// untouched.
func checkStaged(t *testing.T, path string, want []string) {
	t.Helper()

	if want == nil {
		return
	}

	r, err := artifact.Load(path)
	if err != nil {
		t.Fatalf("loading the staged review the post left: %v", err)
	}

	got := make([]string, 0, len(r.Comments))
	for i := range r.Comments {
		got = append(got, r.Comments[i].ID)
	}

	if len(got) != len(want) {
		t.Fatalf("staged comments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("staged comments = %v, want %v", got, want)
		}
	}
}

func TestSummaryAndLabelNameWhatWasSent(t *testing.T) {
	t.Parallel()

	r := review(
		inlineComment("1", artifact.StatusReady),
		artifact.Comment{ID: "2", InReplyTo: 9, Body: "will fix", Status: artifact.StatusTodo},
		artifact.Comment{ID: "3", InReplyTo: 8, Body: "no", Status: artifact.StatusReady, Resolve: true},
		inlineComment("4", artifact.StatusSkip),
	)

	if got, want := post.Summary(r), "posted 1 comment and 2 replies · resolved 1 · skipped 4"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	if got := post.Summary(review()); got != "posted the review" {
		t.Errorf("an empty review summarized as %q", got)
	}
}
