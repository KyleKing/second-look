package brief

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
)

// rule is how wide the line between one comment and the next is drawn.
const rule = 72

// Owed is every comment waiting on an agent, each with the context reading it
// needs, as one document to drain.
//
// It is one file rather than a call per comment because the work is a batch:
// the agent reads the set, does what it can, and answers each one as a turn.
func Owed(r *artifact.Review, d *diff.Diff, ts []threads.Thread) string {
	owed := r.Todos()

	var b strings.Builder

	fmt.Fprintf(&b, "%s/%s#%d at %s\n", r.Owner, r.Repo, r.Number, r.HeadSHA)
	fmt.Fprintf(&b, "%d comment(s) waiting on you.\n", len(owed))

	if len(owed) == 0 {
		return b.String()
	}

	b.WriteString(`
Answer each one with a turn rather than by rewriting the comment:

    second-look comment add ` + fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number) + ` <<'JSON'
    {"comments": [{"id": "<id>", "path": "...", "line": 0, "side": "RIGHT",
      "body": "<the comment as it should now read>", "note": "", "severity": "...",
      "status": "ready", "turn": [{"author": "<you>", "body": "<what you did>"}]}]}
    JSON

Turns append to what is already there, so send only what is new. A comment you
answer is held as a draft for the author to rule on.

`)

	for i := range owed {
		b.WriteString(strings.Repeat("=", rule) + "\n")

		one, err := Comment(r, owed[i].ID, d, ts, 0)
		if err != nil {
			fmt.Fprintf(&b, "%s: %v\n", owed[i].ID, err)

			continue
		}

		b.WriteString(one + "\n")
	}

	return b.String()
}

// Turns is the exchange about one comment, oldest first.
func Turns(c *artifact.Comment) string {
	if len(c.Turns) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("\nTURNS\n")

	for i := range c.Turns {
		fmt.Fprintf(&b, "  @%s\n%s\n", c.Turns[i].Author, indent("  "+c.Turns[i].Body))
	}

	return b.String()
}
