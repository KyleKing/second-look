package brief

import (
	"fmt"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/threads"
)

// Fixes is the prompt that hands the todo replies of a posted review to an
// agent that has not seen the review: each finding with where it is, what it
// said, and what the owner answered.
func Fixes(r *artifact.Review, ts []threads.Thread) string {
	todo := r.TodoReplies()

	var b strings.Builder

	fmt.Fprintf(&b, "%s/%s#%d at %s\n", r.Owner, r.Repo, r.Number, r.HeadSHA)
	fmt.Fprintf(&b, "%s the author agreed to fix. The replies are already posted.\n",
		humanize.Plural(len(todo), "review finding"))

	for i := range todo {
		c := &todo[i]
		path, line := c.Path, c.Line
		finding := ""

		for j := range ts {
			if ts[j].ReplyTo() == c.InReplyTo && len(ts[j].Notes) > 0 {
				path, line, finding = ts[j].Path, ts[j].Line, ts[j].Notes[0].Body
			}
		}

		fmt.Fprintf(&b, "\n%d. %s:%d\n", i+1, path, line)
		fmt.Fprintf(&b, "finding:\n%s\nreply:\n%s\n", indent(finding), indent(c.Body))
	}

	return b.String()
}
