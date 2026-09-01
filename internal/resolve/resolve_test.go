package resolve_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/resolve"
)

// errRefused stands in for whatever gh failed with, which the message has to
// carry through with the conversation named.
var errRefused = errors.New("403")

// recorder keeps the arguments rather than running gh, because what matters is
// which of the two mutations was chosen for each surface.
type recorder struct {
	args []string
	err  error
}

func (r *recorder) Run(_ context.Context, _ string, args ...string) error {
	r.args = args

	return r.err
}

func (r *recorder) joined() string { return strings.Join(r.args, " ") }

// One key marks a conversation dealt with, and what that means depends on the
// surface: a thread resolves, and the two GitHub gives no resolve are
// thumbs-upped, which is the same thing by convention.
func TestRunResolvesAThreadAndReactsToWhatCannotBe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		conv   conversations.Conversation
		want   string
		unwant string
		says   string
	}{
		{
			name: "an inline thread resolves",
			conv: conversations.Conversation{
				Kind: conversations.KindThread, ThreadID: "PRRT_1",
				Repository: "o/r", Number: 3, Path: "a.go", Line: 9,
				Notes: []conversations.Note{{ID: 11, NodeID: "PRRC_11"}},
			},
			want:   "resolveReviewThread",
			unwant: "addReaction",
			says:   "resolved",
		},
		{
			name: "a pull request comment is thumbs-upped",
			conv: conversations.Conversation{
				Kind: conversations.KindComment, Repository: "o/r", Number: 3,
				Notes: []conversations.Note{{ID: 21, NodeID: "IC_21"}},
			},
			want:   "addReaction",
			unwant: "resolveReviewThread",
			says:   "thumbs-upped",
		},
		{
			name: "a review body is thumbs-upped through GraphQL, which is the only way",
			conv: conversations.Conversation{
				Kind: conversations.KindReview, Repository: "o/r", Number: 3,
				Notes: []conversations.Note{{ID: 31, NodeID: "PRR_31"}},
			},
			want:   "addReaction",
			unwant: "reactions",
			says:   "thumbs-upped",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			r := &recorder{}

			status, err := resolve.Run(t.Context(), r, t.TempDir(), &c.conv)
			if err != nil {
				t.Fatal(err)
			}

			if got := r.joined(); !strings.Contains(got, c.want) {
				t.Errorf("called %q, want it to carry %q", got, c.want)
			}

			if got := r.joined(); strings.Contains(got, c.unwant) {
				t.Errorf("called %q, which must not carry %q", got, c.unwant)
			}

			if !strings.Contains(status, c.says) {
				t.Errorf("reported %q, want it to say %q", status, c.says)
			}
		})
	}
}

// A conversation with no node id cannot be reacted to, and saying so beats
// posting a mutation that GitHub refuses.
func TestRunRefusesWithNothingToActOn(t *testing.T) {
	t.Parallel()

	c := conversations.Conversation{
		Kind: conversations.KindComment, Repository: "o/r", Number: 3,
		Notes: []conversations.Note{{ID: 21}},
	}

	if _, err := resolve.Run(t.Context(), &recorder{}, t.TempDir(), &c); !errors.Is(err, resolve.ErrNothingToResolve) {
		t.Fatalf("answered %v, want ErrNothingToResolve", err)
	}
}

// A failure names the conversation, because a footer that says only "gh failed"
// leaves the reader guessing which row it was.
func TestRunNamesTheConversationItFailedOn(t *testing.T) {
	t.Parallel()

	c := conversations.Conversation{
		Kind: conversations.KindThread, ThreadID: "PRRT_1", Repository: "o/r", Number: 3,
		Notes: []conversations.Note{{ID: 11, NodeID: "PRRC_11"}},
	}

	r := &recorder{err: errRefused}

	_, err := resolve.Run(t.Context(), r, t.TempDir(), &c)
	if err == nil || !strings.Contains(err.Error(), "o/r#3") {
		t.Fatalf("answered %v, want it to name o/r#3", err)
	}
}
