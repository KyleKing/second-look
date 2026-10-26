// Package notice is the GitHub notification stream: what is addressed to you
// that the conversation queue does not see — mentions, review requests, a pull
// request of yours somebody answered — triaged without opening a browser.
//
// It reads REST rather than GraphQL because the queue's data is not here: a
// notification knows the subject moved, not which thread inside it did. What
// it adds to the tab is the other half of "owed to me", and the two gestures
// the browser inbox exists for: done, and unsubscribed.
package notice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/ghrun"
)

// Thread is one notification as the queue needs it. Number and Comment are
// zero where the subject names neither — a check run or a release has nothing
// for gh browse to open past the repository.
type Thread struct {
	ID      string    `json:"id"`
	Repo    string    `json:"repo"`
	Number  int       `json:"number,omitempty"`
	Title   string    `json:"title"`
	Kind    string    `json:"kind"`
	Reason  string    `json:"reason"`
	Comment int64     `json:"comment,omitempty"`
	Updated time.Time `json:"updated"`
}

// Key names the row it draws, in a namespace a conversation cannot collide
// with.
func (t Thread) Key() string { return "n:" + t.ID }

// Where is the repository and number as a reader scans for it: owner/name#n
// where the subject names one, and the repository alone where it does not.
func (t Thread) Where() string {
	if t.Number == 0 {
		return t.Repo
	}

	return t.Repo + "#" + strconv.Itoa(t.Number)
}

// Wire is the REST shape /notifications answers with.
type wire struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	UpdatedAt string `json:"updated_at"`
	Subject   struct {
		Title            string `json:"title"`
		URL              string `json:"url"`
		Type             string `json:"type"`
		LatestCommentURL string `json:"latest_comment_url"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// Args is the gh call one fetch makes, exported for the reason
// conversations.LightArgs is: a replay builds the interaction from this rather
// than from a copy that drifts.
//
// Participating narrows the stream to what names you — mentions, review
// requests, threads you are in — rather than everything watched, which is the
// triage the tab exists for.
func Args() []string {
	return []string{"api", "notifications", "-F", "participating=true", "-F", "per_page=50"}
}

// Fetch reads the unread stream in one request.
func Fetch(ctx context.Context, root string) ([]Thread, error) {
	release, err := ghrun.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting gh: %w", err)
	}

	defer release()

	//nolint:gosec // the arguments are constants
	cmd := exec.CommandContext(ctx, "gh", Args()...)
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading your notifications: %w", ghErr(err))
	}

	return Decode(out)
}

// ErrUnavailable is a stream the token cannot read: GitHub answers 404 to a
// token without the notifications scope, and asking again changes nothing.
var ErrUnavailable = errors.New("notifications are not available to this token")

// ghErr puts gh's stderr in the message, which is where its reason lives; the
// exit status alone says nothing.
func ghErr(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		reason := strings.TrimSpace(string(exitErr.Stderr))
		if strings.Contains(reason, "HTTP 404") {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}

		return fmt.Errorf("%w: %s", err, reason)
	}

	return err
}

// Decode reads gh's answer into the queue's rows. A subject URL names its
// pull request or issue at the tail; anything else leaves Number zero.
func Decode(out []byte) ([]Thread, error) {
	var ws []wire
	if err := json.Unmarshal(out, &ws); err != nil {
		return nil, fmt.Errorf("reading the notifications answer: %w", err)
	}

	threads := make([]Thread, 0, len(ws))

	for i := range ws {
		w := &ws[i]
		at, err := time.Parse(time.RFC3339, w.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("reading when %q moved: %w", w.Subject.Title, err)
		}

		threads = append(threads, Thread{
			ID:      w.ID,
			Repo:    w.Repository.FullName,
			Number:  subjectNumber(w.Subject.Type, w.Subject.URL),
			Title:   w.Subject.Title,
			Kind:    w.Subject.Type,
			Reason:  w.Reason,
			Comment: tailNumber64(w.Subject.LatestCommentURL),
			Updated: at,
		})
	}

	return threads, nil
}

// subjectNumber is what gh browse opens for this subject. A pull request or
// issue names itself at the tail of its URL; a check suite's tail is a number
// that browse would misread as a pull request, so it gets none.
func subjectNumber(kind, url string) int {
	switch kind {
	case "PullRequest", "Issue":
		return tailNumber(url)
	default:
		return 0
	}
}

// tailNumber is the last segment of an API URL as a number, which is how a
// subject names its pull request or issue. Zero where the tail is not one.
func tailNumber(url string) int {
	return int(tailNumber64(url))
}

func tailNumber64(url string) int64 {
	at := strings.LastIndex(url, "/")
	if at < 0 {
		return 0
	}

	n, err := strconv.ParseInt(url[at+1:], 10, 64)
	if err != nil {
		return 0
	}

	return n
}

// Done dismisses the thread, which is the browser inbox's checkmark: the row
// leaves the stream and stays findable under Done there.
func Done(ctx context.Context, gh ghrun.Runner, t Thread) error {
	//nolint:wrapcheck // the caller wraps, since it knows the gesture made
	return gh.Run(ctx, ".", "api", "-X", "DELETE", "/notifications/threads/"+t.ID)
}

// Ignore unsubscribes the thread and dismisses it: nothing about it arrives
// again, which is what Done alone cannot promise.
func Ignore(ctx context.Context, gh ghrun.Runner, t Thread) error {
	args := []string{"api", "-X", "PUT", "/notifications/threads/" + t.ID + "/subscription", "-f", "ignored=true"}
	//nolint:wrapcheck // the caller wraps, since it knows the gesture made
	if err := gh.Run(ctx, ".", args...); err != nil {
		return err
	}

	return Done(ctx, gh, t)
}
