package main

import (
	"github.com/kyleking/aragonite/tui/coach"

	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/tui"
)

// mergeVerb is the word the inbox and its next step call M by.
const mergeVerb = "merge"

// inboxRules are the inbox's next steps: a review requested of you is the
// queue's whole reason to exist, so the row under the cursor in that lane
// points at enter.
func inboxRules() []coach.Rule[tui.QueueState] {
	return []coach.Rule[tui.QueueState]{{
		ID: "inbox.review", Action: enterKey, Key: enterKey, What: "review", Tone: coach.Next,
		When: func(s tui.QueueState) bool { return s.OnRow && s.Section == inbox.LaneReview.String() },
		Say:  func(tui.QueueState) string { return "a review is requested of you" },
		Why:  func(tui.QueueState) string { return "enter opens the review screen on it" },
	}, {
		ID: "inbox.merge", Action: "M", Key: "M", What: mergeVerb, Tone: coach.Could,
		When: func(s tui.QueueState) bool { return s.OnRow && s.Section == inbox.LaneReady.String() },
		Say:  func(tui.QueueState) string { return "approved with checks passing" },
		Why:  func(tui.QueueState) string { return "M again confirms" },
	}}
}

// threadsRules point a conversation that moved at reading it.
func threadsRules() []coach.Rule[tui.QueueState] {
	return []coach.Rule[tui.QueueState]{{
		ID: "threads.read", Action: enterKey, Key: enterKey, What: readWord, Tone: coach.Next,
		When: func(s tui.QueueState) bool { return s.RowUnread },
		Say:  func(tui.QueueState) string { return "this conversation moved since you read it" },
		Why:  func(tui.QueueState) string { return "space marks it read without opening it" },
	}}
}

// reviewsRules point a staged review at opening it to finish and post.
func reviewsRules() []coach.Rule[tui.QueueState] {
	return []coach.Rule[tui.QueueState]{{
		ID: "staged.open", Action: enterKey, Key: enterKey, What: "open", Tone: coach.Could,
		When: func(s tui.QueueState) bool { return s.OnRow },
		Say:  func(tui.QueueState) string { return "a review staged here, waiting to post" },
	}}
}
