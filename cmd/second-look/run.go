package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/post"
)

var (
	errNoCommand      = errors.New("no command; try second-look -h")
	errNotAPRNumber   = errors.New("not a pull request number")
	errUnknownCommand = errors.New("unknown command; try second-look -h")
	errUsageComment   = errors.New("usage: second-look comment add <pr>")
	errUsageGet       = errors.New("usage: second-look get <pr>")
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
	case "get":
		return getCmd(ctx, args[1:], stdout)
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

func getCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errUsageGet
	}

	number, err := prNumber(args[0])
	if err != nil {
		return err
	}

	if err := get.Run(ctx, stdout, ".", number); err != nil {
		return fmt.Errorf("get %d: %w", number, err)
	}

	return nil
}

func commentCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "add" {
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

	cached, err := artifact.LoadDiff(".", staged.HeadSHA)
	if err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written: %w", err)
	}

	if err := artifact.Resolve(staged.Comments, diff.Parse(cached)); err != nil {
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

	payloadOnly, err := onlyFlag(args[1:], "--payload", errUsageShow)
	if err != nil {
		return err
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
	if payloadOnly {
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

	dry, err := onlyFlag(args[1:], "--dry-run", errUsagePost)
	if err != nil {
		return err
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	//nolint:wrapcheck // guardAnchors' own error already names what failed
	if err := post.Guard(ctx, ".", r); err != nil {
		return err
	}

	if dry {
		//nolint:wrapcheck // DryRun's own error already names what failed
		return post.DryRun(stdout, r)
	}

	//nolint:wrapcheck // Run's own error already names what failed
	return post.Run(ctx, post.GH(), path, r, stdout)
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
	// Code quoted into an anchor or a body is full of < and >, and escaping them
	// makes the output unreadable for the person and the agent both.
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}

	return nil
}

func artifactPath(pr string) (string, error) {
	number, err := prNumber(pr)
	if err != nil {
		return "", err
	}

	return artifact.Path(".", number), nil
}

func prNumber(pr string) (int, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(pr, "#"))
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("%q is %w", pr, errNotAPRNumber)
	}

	return number, nil
}

// onlyFlag reads the one optional flag a command takes. Anything else is
// refused rather than ignored: a mistyped --dry-run that fell through would
// post the review.
func onlyFlag(args []string, want string, usage error) (bool, error) {
	if len(args) == 1 && args[0] == want {
		return true, nil
	}

	if len(args) == 0 {
		return false, nil
	}

	return false, usage
}
