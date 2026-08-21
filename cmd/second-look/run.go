package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
)

var (
	errNoCommand      = errors.New("no command; try second-look -h")
	errNotAPRNumber   = errors.New("not a pull request number")
	errUnknownCommand = errors.New("unknown command; try second-look -h")
	errUsageComment   = errors.New("usage: second-look comment add <pr>")
	errUsagePost      = errors.New("usage: second-look post <pr> [--dry-run]")
	errUsageShow      = errors.New("usage: second-look show <pr> [--payload]")
)

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errNoCommand
	}

	switch args[0] {
	case "-h":
		return write(stdout, shortHelp)
	case "--help", "help":
		return write(stdout, longHelp)
	case "comment":
		return commentCmd(args[1:], stdin, stdout)
	case "show":
		return showCmd(args[1:], stdout)
	case "post":
		return postCmd(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("%w: %q", errUnknownCommand, args[0])
	}
}

// batch is what an agent writes on stdin. It is the schema's fields, local ones
// included, so drafting evidence and drafting the comment are one call.
type batch struct {
	Note     string             `json:"note,omitempty"`
	Body     string             `json:"body,omitempty"`
	Event    string             `json:"event,omitempty"`
	Comments []artifact.Comment `json:"comments"`
}

func commentCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) < 2 || args[0] != "add" {
		return errUsageComment
	}

	path, err := artifactPath(args[1])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	var b batch

	dec := json.NewDecoder(stdin)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&b); err != nil {
		return fmt.Errorf("reading the batch: %w", err)
	}

	if b.Note != "" {
		r.Note = b.Note
	}
	if b.Body != "" {
		r.Body = b.Body
	}
	if b.Event != "" {
		r.Event = b.Event
	}

	// Validate the whole review before writing any of it, so a bad batch changes
	// nothing rather than landing the comments ahead of the one that failed.
	staged := *r
	staged.Comments = append([]artifact.Comment(nil), r.Comments...)

	for i := range b.Comments {
		staged.Upsert(b.Comments[i])
	}

	if err := staged.Validate(); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	if err := artifact.Save(path, &staged); err != nil {
		return fmt.Errorf("saving the prepared review: %w", err)
	}

	return write(stdout, fmt.Sprintf("%d comment(s) staged, %d total\n", len(b.Comments), len(staged.Comments)))
}

func showCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsageShow
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	// --payload prints exactly what would be sent, so what stays local is
	// inspectable rather than promised.
	if len(args) > 1 && args[1] == "--payload" {
		payload, replies, err := r.Payload()
		if err != nil {
			return fmt.Errorf("building the payload: %w", err)
		}

		return writeJSON(stdout, map[string]any{"review": payload, "replies": replies})
	}

	return writeJSON(stdout, r)
}

func postCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsagePost
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	payload, replies, err := r.Payload()
	if err != nil {
		return fmt.Errorf("building the payload: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding the review: %w", err)
	}

	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", r.Owner, r.Repo, r.Number)
	if len(args) > 1 && args[1] == "--dry-run" {
		return dryRun(stdout, r, endpoint, body, replies)
	}

	if err := ghPost(ctx, endpoint, body); err != nil {
		return err
	}

	if err := write(stdout, "posted "+endpoint+"\n"); err != nil {
		return err
	}

	return postReplies(ctx, r, replies)
}

func dryRun(stdout io.Writer, r *artifact.Review, endpoint string, body []byte, replies []artifact.ReplyPayload) error {
	if err := write(stdout, fmt.Sprintf("POST %s\n%s\n", endpoint, body)); err != nil {
		return err
	}

	for _, reply := range replies {
		line := fmt.Sprintf("POST %s\n", replyEndpoint(r, reply.InReplyTo))
		if err := write(stdout, line); err != nil {
			return err
		}
	}

	return nil
}

// postReplies runs after the review is already posted, which is why a failure
// here is reported with that fact rather than retried.
func postReplies(ctx context.Context, r *artifact.Review, replies []artifact.ReplyPayload) error {
	for _, reply := range replies {
		rb, err := json.Marshal(reply)
		if err != nil {
			return fmt.Errorf("encoding a reply: %w", err)
		}

		if err := ghPost(ctx, replyEndpoint(r, reply.InReplyTo), rb); err != nil {
			return fmt.Errorf("the review posted but a reply did not: %w", err)
		}
	}

	return nil
}

func replyEndpoint(r *artifact.Review, commentID int64) string {
	return fmt.Sprintf("/repos/%s/%s/pulls/comments/%d/replies", r.Owner, r.Repo, commentID)
}

func ghPost(ctx context.Context, endpoint string, body []byte) error {
	//nolint:gosec // the endpoint is built from the artifact's own owner, repo, and number
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "POST", endpoint, "--input", "-")
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh api POST %s: %w", endpoint, err)
	}

	return nil
}

func write(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}

	return nil
}

func artifactPath(pr string) (string, error) {
	var number int
	if _, err := fmt.Sscanf(strings.TrimPrefix(pr, "#"), "%d", &number); err != nil {
		return "", fmt.Errorf("%q is %w", pr, errNotAPRNumber)
	}

	return artifact.Path(".", number), nil
}
