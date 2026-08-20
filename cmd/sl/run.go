package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
)

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("no command; try sl -h")
	}

	switch args[0] {
	case "-h":
		fmt.Fprint(stdout, shortHelp)

		return nil
	case "--help", "help":
		fmt.Fprint(stdout, longHelp)

		return nil
	case "comment":
		return commentCmd(args[1:], stdin, stdout)
	case "show":
		return showCmd(args[1:], stdout)
	case "post":
		return postCmd(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q; try sl -h", args[0])
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
		return errors.New("usage: sl comment add <pr>")
	}

	path, err := artifactPath(args[1])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return err
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
	for _, c := range b.Comments {
		staged.Upsert(c)
	}
	if err := staged.Validate(); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	if err := artifact.Save(path, &staged); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%d comment(s) staged, %d total\n", len(b.Comments), len(staged.Comments))

	return nil
}

func showCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: sl show <pr> [--payload]")
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return err
	}

	// --payload prints exactly what would be sent, so what stays local is
	// inspectable rather than promised.
	if len(args) > 1 && args[1] == "--payload" {
		payload, replies, err := r.Payload()
		if err != nil {
			return err
		}

		return writeJSON(stdout, map[string]any{"review": payload, "replies": replies})
	}

	return writeJSON(stdout, r)
}

func postCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: sl post <pr> [--dry-run]")
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return err
	}

	payload, replies, err := r.Payload()
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", r.Owner, r.Repo, r.Number)
	if len(args) > 1 && args[1] == "--dry-run" {
		fmt.Fprintf(stdout, "POST %s\n%s\n", endpoint, body)
		for _, reply := range replies {
			fmt.Fprintf(stdout, "POST /repos/%s/%s/pulls/comments/%d/replies\n",
				r.Owner, r.Repo, reply.InReplyTo)
		}

		return nil
	}

	if err := ghPost(endpoint, body); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "posted %s\n", endpoint)

	// Replies go after the review. A failure here leaves the review posted, which
	// is why it is reported rather than retried.
	for _, reply := range replies {
		rb, err := json.Marshal(reply)
		if err != nil {
			return err
		}

		ep := fmt.Sprintf("/repos/%s/%s/pulls/comments/%d/replies", r.Owner, r.Repo, reply.InReplyTo)
		if err := ghPost(ep, rb); err != nil {
			return fmt.Errorf("the review posted but a reply did not: %w", err)
		}
	}

	return nil
}

func ghPost(endpoint string, body []byte) error {
	cmd := exec.Command("gh", "api", "--method", "POST", endpoint, "--input", "-")
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return enc.Encode(v)
}

func artifactPath(pr string) (string, error) {
	var number int
	if _, err := fmt.Sscanf(strings.TrimPrefix(pr, "#"), "%d", &number); err != nil {
		return "", fmt.Errorf("%q is not a pull request number", pr)
	}

	return artifact.Path(".", number), nil
}
