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
	endpoints []string
	bodies    [][]byte
	failOn    int
	failDel   bool
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
		name, answer = "graphql addReply",
			`{"data":{"addPullRequestReviewThreadReply":{"comment":{"databaseId":777}}}}`
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
			p := &fakePoster{failOn: tt.failOn, failDel: tt.failDel}
			var out bytes.Buffer

			err := post.Run(context.Background(), p, path, tt.review, &out)

			checkErr(t, err, tt.wantErr)
			checkEndpoints(t, p.endpoints, tt.wantEndpoints)
			checkArtifact(t, path, tt.wantFileGone)
			checkRecord(t, path, tt.wantRecord)
		})
	}
}

type runCase struct {
	name          string
	review        *artifact.Review
	failOn        int
	failDel       bool
	wantErr       string
	wantEndpoints []string
	wantFileGone  bool
	wantRecord    bool
}

func runCases() []runCase {
	return append(sentCases(), blockedCases()...)
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
			name:   "happy path opens a pending review, adds the reply, and submits once",
			review: review(inlineComment("1", artifact.StatusReady), reply),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql addReply", "graphql submit",
			},
			wantFileGone: true,
			wantRecord:   true,
		},
		{
			name:    "a reply failure abandons the pending review rather than leaving it invisible",
			review:  review(inlineComment("1", artifact.StatusReady), reply),
			failOn:  3,
			wantErr: "the pending review it left was deleted",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql addReply",
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
				"graphql pullRequest", "graphql addReview", "graphql addReply",
				"delete /repos/kyleking/second-look/pulls/42/reviews/555",
			},
		},
		{
			name:   "a resolving reply resolves its thread after the submit",
			review: review(reply, wontFix),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview",
				"graphql addReply", "graphql addReply", "graphql submit",
				"resolve PRRT_9",
			},
			wantFileGone: true,
			wantRecord:   true,
		},
		{
			name:    "a thread that will not resolve still leaves the posted record",
			review:  review(wontFix),
			failOn:  5,
			wantErr: "the review posted as 555; the reply posted but its thread did not resolve",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql addReply",
				"graphql submit", "resolve PRRT_9",
			},
			wantFileGone: true,
			wantRecord:   true,
		},
		{
			name: "a reply without a thread id looks the thread up by the comment it answers",
			review: review(inlineComment("1", artifact.StatusReady),
				artifact.Comment{ID: "6", InReplyTo: 44, Body: "on it", Status: artifact.StatusReady}),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql threads",
				"graphql addReply", "graphql submit",
			},
			wantFileGone: true,
			wantRecord:   true,
		},
		{
			name:    "a reply answering nothing on the pull request is refused",
			review:  review(artifact.Comment{ID: "7", InReplyTo: 9999, Body: "huh", Status: artifact.StatusReady}),
			wantErr: "not a thread on this pull request",
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql threads",
				"delete /repos/kyleking/second-look/pulls/42/reviews/555",
			},
		},
		{
			name: "a todo reply posts like any reply",
			review: review(artifact.Comment{
				ID: "5", InReplyTo: 11, Body: "will fix",
				Status: artifact.StatusTodo, ThreadID: "PRRT_2",
			}),
			wantEndpoints: []string{
				"graphql pullRequest", "graphql addReview", "graphql addReply", "graphql submit",
			},
			wantFileGone: true,
			wantRecord:   true,
		},
	}
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

// checkRecord asserts the posted record exists under posted/ and names the
// id the review landed as.
func checkRecord(t *testing.T, path string, want bool) {
	t.Helper()

	record := filepath.Join(filepath.Dir(path), "posted", "pr-42-555.toml")

	info, err := os.Stat(record)
	switch {
	case want && err != nil:
		t.Fatalf("no posted record at %s: %v", record, err)
	case !want && err == nil:
		t.Fatalf("a posted record exists after a failed post: %s", record)
	case want && info.Size() == 0:
		t.Fatalf("the posted record at %s is empty", record)
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
