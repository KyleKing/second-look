package post

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
)

// The review posts through GraphQL rather than REST for one reason: a reply
// posted to the REST replies endpoint lands in whatever pending review the
// author has open, which may not be the one that just submitted — it returns
// 200 and stays invisible until a review nobody knows about is submitted. The
// reply mutation has the same trap, so the answer is asked which review took
// the comment: anything but PENDING means it published on its own, and PENDING
// means a pending review captured it and deleteComment puts it back.
const (
	prNodeQuery = `query($owner:String!,$name:String!,$number:Int!){
  repository(owner:$owner,name:$name){pullRequest(number:$number){id}}
}`

	repoIDQuery = `query($owner:String!,$name:String!){
  repository(owner:$owner,name:$name){databaseId}
}`

	addReviewMutation = `mutation($input:AddPullRequestReviewInput!){
  addPullRequestReview(input:$input){pullRequestReview{id databaseId}}
}`

	addReplyMutation = `mutation($input:AddPullRequestReviewThreadReplyInput!){
  addPullRequestReviewThreadReply(input:$input){
    comment{id databaseId pullRequestReview{databaseId state}}
  }
}`

	deleteCommentMutation = `mutation($input:DeletePullRequestReviewCommentInput!){
  deletePullRequestReviewComment(input:$input){pullRequestReviewComment{id}}
}`

	submitReviewMutation = `mutation($input:SubmitPullRequestReviewInput!){
  submitPullRequestReview(input:$input){pullRequestReview{id databaseId}}
}`

	// ThreadQuery finds the node id of the thread a hand-staged reply answers,
	// which the reply's in_reply_to names by comment database id instead.
	threadQuery = `query($owner:String!,$name:String!,$number:Int!,$after:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$number){
      reviewThreads(first:100,after:$after){
        pageInfo{hasNextPage endCursor}
        nodes{id comments(first:1){nodes{databaseId}}}
      }
    }
  }
}`
)

// errGraphQLFailed wraps the messages a GraphQL answer's errors half carries,
// so a caller can still ask errors.Is about a failed call.
var errGraphQLFailed = errors.New("the GraphQL call failed")

// errNoPullRequest says the pull request query answered nothing, which for a
// review read off a pr-*.toml means the artifact names a number that is not a
// pull request.
var errNoPullRequest = errors.New("no pull request")

// errNoReviewAnswered says a mutation returned success without the review it
// was supposed to open or submit, which is GitHub answering out of shape.
var errNoReviewAnswered = errors.New("GitHub answered no review")

// errNoCommentAnswered says addPullRequestReviewThreadReply returned success
// without the comment it was supposed to add.
var errNoCommentAnswered = errors.New("GitHub answered no comment")

// errNotAThread says a reply's in_reply_to names a comment that opens no
// thread on the pull request, so there is nothing to attach the reply to.
var errNotAThread = errors.New("not a thread on this pull request")

// errReplyCaptured says a pending review the author has open elsewhere took
// the reply instead of it publishing. AddReply deletes the comment back out
// before this is returned, so nothing is left sitting invisibly in it.
var errReplyCaptured = errors.New("a pending review on this pull request captured the reply")

// The GraphQL variables every mutation shares a name for.
const (
	keyInput                = "input"
	keyOwner                = "owner"
	keyPullRequestReviewID  = "pullRequestReviewId"
	keyPullRequestReviewTID = "pullRequestReviewThreadId"
	keyBody                 = "body"
)

// request is one call to gh api graphql --input -.
func request(query string, variables any) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, fmt.Errorf("encoding the request: %w", err)
	}

	return body, nil
}

// graphql answers the data half of a GraphQL response into out, with the
// errors half as the error. GitHub can answer both, so errors win.
func graphql(out any, body []byte) error {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}

	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}

		return fmt.Errorf("%w: %s", errGraphQLFailed, strings.Join(msgs, "; "))
	}

	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}

	return nil
}

// repoID is the repository's numeric id, which the attachment upload names in
// its query — the node id it also has is a different spelling the endpoint
// does not take.
func repoID(ctx context.Context, p Poster, owner, repo string) (int64, error) {
	body, err := request(repoIDQuery, map[string]any{keyOwner: owner, keyName: repo})
	if err != nil {
		return 0, err
	}

	raw, err := p.GraphQL(ctx, body)
	if err != nil {
		return 0, fmt.Errorf("reading the repository: %w", err)
	}

	//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
	var out struct {
		Repository struct {
			DatabaseID int64 `json:"databaseId"`
		} `json:"repository"`
	}

	if err := graphql(&out, raw); err != nil {
		return 0, fmt.Errorf("reading the repository: %w", err)
	}

	return out.Repository.DatabaseID, nil
}

// prNodeID is the pull request's GraphQL id, which every mutation on it names.
func prNodeID(ctx context.Context, p Poster, r *artifact.Review) (string, error) {
	body, err := request(prNodeQuery, map[string]any{
		keyOwner: r.Owner, keyName: r.Repo, "number": r.Number,
	})
	if err != nil {
		return "", err
	}

	raw, err := p.GraphQL(ctx, body)
	if err != nil {
		return "", fmt.Errorf("reading the pull request: %w", err)
	}

	//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
	var out struct {
		Repository struct {
			PullRequest struct {
				ID string `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	if err := graphql(&out, raw); err != nil {
		return "", fmt.Errorf("reading the pull request: %w", err)
	}

	if out.Repository.PullRequest.ID == "" {
		return "", fmt.Errorf("reading the pull request: %w %s/%s#%d",
			errNoPullRequest, r.Owner, r.Repo, r.Number)
	}

	return out.Repository.PullRequest.ID, nil
}

// pendingReview is what addPullRequestReview answered: the review everything
// else hangs off. Its databaseId is what the cleanup delete and the posted
// record name, since neither speaks GraphQL ids.
//
//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type pendingReview struct {
	ID         string `json:"id"`
	DatabaseID int64  `json:"databaseId"`
}

// openReview creates the pending review holding this review's comments, and
// replies attach to it by id rather than to whichever pending review happens
// to exist.
func openReview(
	ctx context.Context, p Poster, prID string, r *artifact.Review, threads []artifact.DraftThread,
) (*pendingReview, error) {
	body, err := request(addReviewMutation, map[string]any{keyInput: map[string]any{
		"pullRequestId": prID,
		"commitOID":     r.HeadSHA,
		"threads":       threads,
	}})
	if err != nil {
		return nil, err
	}

	raw, err := p.GraphQL(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("opening the review: %w", err)
	}

	//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
	var out struct {
		AddPullRequestReview struct {
			PullRequestReview pendingReview `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}

	if err := graphql(&out, raw); err != nil {
		return nil, fmt.Errorf("opening the review: %w", err)
	}

	if out.AddPullRequestReview.PullRequestReview.ID == "" {
		return nil, fmt.Errorf("opening the review: %w", errNoReviewAnswered)
	}

	return &out.AddPullRequestReview.PullRequestReview, nil
}

// threadIDs answers the node id of every thread on the pull request, keyed by
// the database id of the comment that opened it — which is what a reply's
// in_reply_to holds. It is only asked when a reply does not name its thread.
func threadIDs(ctx context.Context, p Poster, r *artifact.Review) (map[int64]string, error) {
	out := map[int64]string{}
	after := ""

	for {
		body, err := request(threadQuery, map[string]any{
			keyOwner: r.Owner, keyName: r.Repo, "number": r.Number,
			"after": after,
		})
		if err != nil {
			return nil, err
		}

		raw, err := p.GraphQL(ctx, body)
		if err != nil {
			return nil, fmt.Errorf("reading the pull request's threads: %w", err)
		}

		//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
		var page struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID       string `json:"id"`
							Comments struct {
								Nodes []struct {
									DatabaseID int64 `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}

		if err := graphql(&page, raw); err != nil {
			return nil, fmt.Errorf("reading the pull request's threads: %w", err)
		}

		for _, t := range page.Repository.PullRequest.ReviewThreads.Nodes {
			if len(t.Comments.Nodes) > 0 {
				out[t.Comments.Nodes[0].DatabaseID] = t.ID
			}
		}

		if !page.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage {
			return out, nil
		}

		after = page.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor
	}
}

// addReply posts one reply on its thread with no review named — naming one
// files the reply under it, and the conversation view then draws it a second
// time as a lone comment on the line. The answer's review state guards the
// other side of that: PENDING means a pending review captured the reply
// instead of publishing it, which deleteComment undoes. It answers the
// comment's GraphQL and database ids.
func addReply(
	ctx context.Context, p Poster, threadID, body string,
) (string, int64, error) {
	req, err := request(addReplyMutation, map[string]any{keyInput: map[string]any{
		keyPullRequestReviewTID: threadID,
		keyBody:                 body,
	}})
	if err != nil {
		return "", 0, err
	}

	raw, err := p.GraphQL(ctx, req)
	if err != nil {
		return "", 0, fmt.Errorf("adding a reply: %w", err)
	}

	//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
	var out struct {
		AddPullRequestReviewThreadReply struct {
			Comment struct {
				ID         string `json:"id"`
				DatabaseID int64  `json:"databaseId"`
				Review     struct {
					DatabaseID int64  `json:"databaseId"`
					State      string `json:"state"`
				} `json:"pullRequestReview"`
			} `json:"comment"`
		} `json:"addPullRequestReviewThreadReply"`
	}

	if err := graphql(&out, raw); err != nil {
		return "", 0, fmt.Errorf("adding a reply: %w", err)
	}

	comment := out.AddPullRequestReviewThreadReply.Comment
	if comment.DatabaseID == 0 {
		return "", 0, fmt.Errorf("adding a reply: %w", errNoCommentAnswered)
	}

	if comment.Review.State == "PENDING" {
		return "", 0, uncapture(ctx, p, comment.ID, comment.Review.DatabaseID)
	}

	return comment.ID, comment.DatabaseID, nil
}

// uncapture deletes a reply back out of the pending review that took it. The
// review is left alone — it may hold comments still being drafted — and a
// failed delete names where the reply is sitting.
func uncapture(ctx context.Context, p Poster, commentID string, reviewID int64) error {
	if err := deleteComment(ctx, p, commentID); err != nil {
		return fmt.Errorf("%w; it is sitting in pending review %d and removing it failed: %w",
			errReplyCaptured, reviewID, err)
	}

	return fmt.Errorf("%w; it was removed — submit or discard that review on GitHub, then post again",
		errReplyCaptured)
}

// deleteComment removes a comment from a pending review, the only place a
// deletable review comment lives — a published one is permanent.
func deleteComment(ctx context.Context, p Poster, commentID string) error {
	req, err := request(deleteCommentMutation, map[string]any{keyInput: map[string]any{
		"id": commentID,
	}})
	if err != nil {
		return err
	}

	raw, err := p.GraphQL(ctx, req)
	if err != nil {
		return fmt.Errorf("deleting the captured reply: %w", err)
	}

	//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
	var out struct {
		DeletePullRequestReviewComment struct {
			Comment struct {
				ID string `json:"id"`
			} `json:"pullRequestReviewComment"`
		} `json:"deletePullRequestReviewComment"`
	}

	if err := graphql(&out, raw); err != nil {
		return fmt.Errorf("deleting the captured reply: %w", err)
	}

	return nil
}

// submitReview submits the pending review, which is when its comments and its
// replies all become visible at once.
func submitReview(
	ctx context.Context, p Poster, reviewID, event, body string,
) (*pendingReview, error) {
	req, err := request(submitReviewMutation, map[string]any{keyInput: map[string]any{
		keyPullRequestReviewID: reviewID,
		"event":                event,
		keyBody:                body,
	}})
	if err != nil {
		return nil, err
	}

	raw, err := p.GraphQL(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("submitting the review: %w", err)
	}

	//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
	var out struct {
		SubmitPullRequestReview struct {
			PullRequestReview pendingReview `json:"pullRequestReview"`
		} `json:"submitPullRequestReview"`
	}

	if err := graphql(&out, raw); err != nil {
		return nil, fmt.Errorf("submitting the review: %w", err)
	}

	if out.SubmitPullRequestReview.PullRequestReview.ID == "" {
		return nil, fmt.Errorf("submitting the review: %w", errNoReviewAnswered)
	}

	return &out.SubmitPullRequestReview.PullRequestReview, nil
}
