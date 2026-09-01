package conversations

import (
	"sort"
	"strings"
)

// Bucket is one section of the queue, in the order they want doing.
type Bucket struct {
	Name  string         `json:"bucket"`
	Items []Conversation `json:"items"`
}

// The bucket names, in queue order.
const (
	BucketNew      = "new since you looked"
	BucketWaiting  = "waiting on you"
	BucketAwaiting = "awaiting others"
)

// Buckets sorts the queue into what moved since you last looked, what is still
// your turn, and what is somebody else's.
//
// The first two overlap by definition, and the split is the point: a discussion
// that moved is the one nobody told you about, while one you have already read
// and not answered is a thing you are putting off. A conversation lands in the
// first bucket it answers to.
func Buckets(q *Queue, looked *Looked) []Bucket {
	out := []Bucket{
		{Name: BucketNew},
		{Name: BucketWaiting},
		{Name: BucketAwaiting},
	}

	for i := range q.Conversations {
		c := q.Conversations[i]

		switch {
		case mine(&c, q.Viewer):
			out[2].Items = append(out[2].Items, c)
		case looked.Since(&c):
			out[1].Items = append(out[1].Items, c)
		default:
			out[0].Items = append(out[0].Items, c)
		}
	}

	for i := range out {
		byRecency(out[i].Items)
	}

	return out
}

// mine reports whether the last word is yours, which is what makes a
// conversation somebody else's turn.
func mine(c *Conversation, me string) bool {
	return strings.EqualFold(c.Last().Author, me)
}

// byRecency puts the conversation that moved most recently first, because
// recency is what decides between two rows that are otherwise equal.
func byRecency(cs []Conversation) {
	sort.SliceStable(cs, func(i, j int) bool {
		return cs[i].Updated().After(cs[j].Updated())
	})
}

// Count is how many conversations are in every bucket.
func Count(bs []Bucket) int {
	n := 0
	for i := range bs {
		n += len(bs[i].Items)
	}

	return n
}
