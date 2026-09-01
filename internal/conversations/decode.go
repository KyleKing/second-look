package conversations

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type response struct {
	Data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
		Search struct {
			Nodes []pullRequest `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type pullRequest struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	IsDraft    bool   `json:"isDraft"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Author        author `json:"author"`
	ReviewThreads struct {
		Nodes []thread `json:"nodes"`
	} `json:"reviewThreads"`
	Comments struct {
		Nodes []comment `json:"nodes"`
	} `json:"comments"`
	Reviews struct {
		Nodes []review `json:"nodes"`
	} `json:"reviews"`
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type author struct {
	Login string `json:"login"`
	// Type is GitHub's own kind for the account. "Bot" is an App.
	Type string `json:"__typename"`
}

// machine is the account kind GitHub gives a GitHub App.
const machine = "Bot"

func (a author) bot() bool { return a.Type == machine }

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type thread struct {
	ID         string `json:"id"`
	IsResolved bool   `json:"isResolved"`
	IsOutdated bool   `json:"isOutdated"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	DiffSide   string `json:"diffSide"`
	Comments   struct {
		Nodes []comment `json:"nodes"`
	} `json:"comments"`
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type comment struct {
	NodeID         string          `json:"id"`
	DatabaseID     int64           `json:"databaseId"`
	Body           string          `json:"body"`
	CreatedAt      time.Time       `json:"createdAt"`
	Author         author          `json:"author"`
	ReactionGroups []reactionGroup `json:"reactionGroups"`
}

//nolint:tagliatelle // GraphQL answers in camelCase and these names are GitHub's
type reactionGroup struct {
	Content          string `json:"content"`
	ViewerHasReacted bool   `json:"viewerHasReacted"`
}

type review struct {
	comment

	State string `json:"state"`
}

// thumbsUp is the reaction that stands in for a resolve on the two surfaces
// GitHub gives no resolve.
const thumbsUp = "THUMBS_UP"

// Decode reads what the query answered and keeps the conversations that are
// yours. It is exported because a test that seeds the queue reads the same
// recording the fetcher does, and two copies of GitHub's shape would drift.
func Decode(body []byte) (*Queue, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("reading your open conversations: %w", err)
	}

	me := r.Data.Viewer.Login
	if me == "" {
		return nil, ErrNoViewer
	}

	q := &Queue{Viewer: me}

	for i := range r.Data.Search.Nodes {
		q.Conversations = append(q.Conversations, r.Data.Search.Nodes[i].conversations(me)...)
	}

	return q, nil
}

func (p *pullRequest) conversations(me string) []Conversation {
	yours := p.Author.Login == me

	var out []Conversation

	for i := range p.ReviewThreads.Nodes {
		if c, ok := p.thread(&p.ReviewThreads.Nodes[i], me, yours); ok {
			out = append(out, c)
		}
	}

	for i := range p.Comments.Nodes {
		if c, ok := p.single(KindComment, &p.Comments.Nodes[i], me, yours); ok {
			out = append(out, c)
		}
	}

	for i := range p.Reviews.Nodes {
		rev := &p.Reviews.Nodes[i]
		// A pending review has not been submitted, so nobody else can see it,
		// and an approval with no body said nothing to answer.
		if rev.State == "PENDING" || strings.TrimSpace(rev.Body) == "" {
			continue
		}

		if c, ok := p.single(KindReview, &rev.comment, me, yours); ok {
			out = append(out, c)
		}
	}

	return out
}

func (p *pullRequest) thread(t *thread, me string, yours bool) (Conversation, bool) {
	if t.IsResolved || len(t.Comments.Nodes) == 0 {
		return Conversation{}, false
	}

	c := p.base(KindThread, yours)
	c.ThreadID = t.ID
	c.Path = t.Path
	c.Line = t.Line
	c.Side = t.DiffSide
	c.Outdated = t.IsOutdated
	// A thumbs-up on the comment that raised the point is the same statement as a
	// resolve, and it is the marker that survives when the resolve is not mine to
	// make.
	c.Handled = t.Comments.Nodes[0].reacted()
	if c.Handled {
		return Conversation{}, false
	}

	for i := range t.Comments.Nodes {
		c.Notes = append(c.Notes, t.Comments.Nodes[i].note())
	}

	c.Why = why(yours, me, c.Notes)

	return c, c.Why.Any()
}

// single is a conversation that is one comment: a pull request comment or a
// review body. GitHub keeps no thread around either, so each is its own row and
// the reaction on it is what says it has been dealt with.
//
// A machine account reaches the queue only through an inline review thread,
// which is the one surface where what it says is anchored to code and can be
// resolved. Its pull request comments are status posts -- a coverage table, a
// linkback, a nudge -- that nobody resolves or reacts to, so admitting them
// would fill the queue with rows that never leave it.
func (p *pullRequest) single(k Kind, cm *comment, me string, yours bool) (Conversation, bool) {
	// Your own comment with nothing under it is a thing you said, not a
	// discussion: nobody owes an answer, and you will not thumbs-up yourself, so
	// admitting it would add a row that can never leave.
	if cm.Author.bot() || strings.EqualFold(cm.Author.Login, me) {
		return Conversation{}, false
	}

	c := p.base(k, yours)
	c.Notes = []Note{cm.note()}
	c.Handled = cm.reacted()
	c.Why = why(yours, me, c.Notes)

	if c.Handled {
		return Conversation{}, false
	}

	return c, c.Why.Any()
}

func (p *pullRequest) base(k Kind, yours bool) Conversation {
	return Conversation{
		Kind:       k,
		Repository: p.Repository.NameWithOwner,
		Number:     p.Number,
		Title:      p.Title,
		URL:        p.URL,
		Why:        Why{Yours: yours},
	}
}

func (c *comment) note() Note {
	return Note{
		ID:      c.DatabaseID,
		NodeID:  c.NodeID,
		Author:  c.Author.Login,
		Body:    c.Body,
		Created: c.CreatedAt,
	}
}

func (c *comment) reacted() bool {
	for _, g := range c.ReactionGroups {
		if g.Content == thumbsUp && g.ViewerHasReacted {
			return true
		}
	}

	return false
}

// why records every reason a conversation is yours. A mention is matched
// case-insensitively against @login, because GitHub resolves a handle that way
// and a comment that writes it differently still reached you.
func why(yours bool, me string, notes []Note) Why {
	w := Why{Yours: yours}
	handle := "@" + strings.ToLower(me)

	for i := range notes {
		if strings.EqualFold(notes[i].Author, me) {
			w.Spoke = true
		}

		if strings.Contains(strings.ToLower(notes[i].Body), handle) {
			w.Mentioned = true
		}
	}

	return w
}
