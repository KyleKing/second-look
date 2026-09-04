package prstate_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/prstate"
)

// What the row says has to lead with the thing that makes the staged review
// moot, then with your own last word on it: a queue that says "approved" when
// somebody else approved is a queue that gets a second approval.
func TestWhatARowSaysAboutAPullRequest(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "merged while the review sat here",
			body: `{"data":{"repository":{"pullRequest":{"state":"MERGED",` +
				`"reviewDecision":"APPROVED","viewerLatestReview":{"state":"APPROVED"}}}}}`,
			want: "merged",
		},
		{
			name: "you approved it",
			body: `{"data":{"repository":{"pullRequest":{"state":"OPEN",` +
				`"reviewDecision":"APPROVED","viewerLatestReview":{"state":"APPROVED"}}}}}`,
			want: "you approved it",
		},
		{
			name: "somebody else approved it",
			body: `{"data":{"repository":{"pullRequest":{"state":"OPEN",` +
				`"reviewDecision":"APPROVED","viewerLatestReview":null}}}}`,
			want: "approved",
		},
		{
			name: "open and nobody has reviewed it",
			body: `{"data":{"repository":{"pullRequest":{"state":"OPEN",` +
				`"reviewDecision":"REVIEW_REQUIRED","viewerLatestReview":null}}}}`,
			want: "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := prstate.Decode([]byte(c.body))
			if err != nil {
				t.Fatalf("reading the state: %v", err)
			}

			if word := got.Word(); word != c.want {
				t.Errorf("the row says %q, want %q", word, c.want)
			}
		})
	}
}
