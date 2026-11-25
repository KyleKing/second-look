package tui

import (
	"slices"

	"github.com/kyleking/aragonite/tui/keys"
)

// The review screen's own actions, named the way a legend or a moved key's
// notice says them. They mirror defaultKeyMap rather than drive it: the screen
// dispatches through bubbles bindings and its own chords, and this table is
// what the family's shared keymap is checked against, so a key cannot drift
// from what it means in jojo and jj-diff without a test saying so.
const (
	actEdit         = "edit"
	actNote         = "note-edit"
	actFixme        = "fixme"
	actCheckout     = "checkout"
	actState        = "state"
	actReady        = "ready"
	actDraft        = "draft"
	actSkip         = "skip"
	actTodo         = "todo"
	actResolve      = "resolve"
	actAgent        = "agent"
	actTrouble      = "trouble"
	actLockfile     = "lockfile"
	actLabels       = "labels"
	actOrder        = "order"
	actTour         = "tour"
	actSuggest      = "suggest"
	actAsk          = "ask"
	actReact        = "react"
	actBrowse       = "browse"
	actMerge        = "merge"
	actAccept       = "accept"
	actNextDecision = "decision-next"
	actPrevDecision = "decision-prev"
	actFoldOpen     = "fold-open"
	actFoldClose    = "fold-close"
	actViewThreads  = "view-threads"
	actViewComments = "view-comments"
	actViewPreset   = "view-preset"
)

// How many keys of their own the review and list mirrors carry, which their
// slices are sized by.
const (
	reviewOwn = 29
	listOwn   = 17
)

// The list screens' own actions.
const (
	actChoose  = "choose"
	actDiscard = "discard"
	actUnfocus = "unfocus"
	actScope   = "scope"
	actTabPick = "queue-pick"
	actSection = "section-next"
	actClean   = "clean"
	actStale   = "clean-stale"
	actApprove = "approve"
)

// sharedReview is the shared table as the review screen has it: what it has
// no concept of is left out (undo and redo, panes, label jumps, whole pages,
// the old and new sides, messages, line numbers and word refinement as
// toggles, conflicts, and the queue object a list steps), so those keys stay
// unbound here rather than meaning something else.
func sharedReview() []keys.Binding {
	return keys.Without(slices.Concat(keys.Global(), keys.DiffView()),
		keys.ActionUndo, keys.ActionRedo, keys.ActionSwitchPane, keys.ActionLabelJump,
		keys.ActionPageDown, keys.ActionPageUp, keys.ActionSideOld, keys.ActionSideNew,
		keys.ActionMessages, keys.ActionViewNumbers, keys.ActionViewRefine,
		keys.NextAction(keys.ObjConflict), keys.PrevAction(keys.ObjConflict),
		keys.NextAction(keys.ObjQueue), keys.PrevAction(keys.ObjQueue),
	)
}

// familyReview is the review screen's own keys, on top of sharedReview and
// the family verbs it has (i insert, P publish, bb and bd the branches, gi the
// card about the pull request).
func familyReview() []keys.Binding {
	own := func(action, seq, what string) keys.Binding {
		return keys.Binding{Action: action, Keys: []string{seq}, What: what, Group: "Review", Contexts: keys.View}
	}

	out := make([]keys.Binding, 0, reviewOwn+len(keys.Verbs()))
	out = append(out,
		own(actEdit, "e", "edit what is here"),
		own(actNote, "E", "edit a note"),
		own(actFixme, "F", "fixme into the source"),
		own(actCheckout, "C", "check out"),
		own(actState, "m", "a state, then r d t x"),
		own(actReady, "r", "ready"),
		own(actDraft, "d", "draft"),
		own(actSkip, "x", "skip"),
		own(actTodo, "t", "todo"),
		own(actResolve, "R", "resolve"),
		own(actAgent, "T", "the agent"),
		own(actTrouble, "X", "trouble"),
		own(actLockfile, "L", "ask about a lockfile"),
		own(actLabels, "l", "labels"),
		own(actOrder, "O", "the diff's own order"),
		own(actTour, "p", "the tour"),
		own(actSuggest, "s", "a suggestion"),
		own(actAsk, "A", "ask the agent"),
		own(actReact, ",", "a reaction"),
		own(actBrowse, "o", "on GitHub"),
		own(actMerge, "M", "merge"),
		own(actAccept, enterKey, "accept"),
		own(actNextDecision, tabKey, "what wants a decision"),
		own(actPrevDecision, "shift+tab", "and back"),
		own(actFoldOpen, "z o", "open this"),
		own(actFoldClose, "z c", "close this"),
		own(actViewThreads, `\ t`, "threads"),
		own(actViewComments, `\ c`, "the comments"),
		own(actViewPreset, `\ p`, "the next preset of looks"),
	)

	return append(out, keys.Verbs()...)
}

// reviewMoves are this release's moved review keys, each answering where its
// job went. The next minor drops them, and the reserved ones stay unbound.
func reviewMoves() keys.Moves {
	return keys.Moves{
		"a":   keys.ActionInsert,
		"S":   keys.ActionPublish,
		"B":   keys.ActionBranches,
		"D":   keys.ActionBranchDelete,
		ctrlR: keys.ActionReload,
		"y":   keys.ActionScrollUp,
		"u":   keys.ActionViewCycle,
		"c":   actViewComments,
	}
}

// sharedList is the shared table as the list screens have it: moving, the
// read tick and the unread and queue motions widened to a list, the fold
// toggle on a lane's heading, and the general keys a queue has.
func sharedList() []keys.Binding {
	out := keys.Without(slices.Concat(keys.Global(), keys.DiffView()),
		keys.ActionUndo, keys.ActionRedo, keys.ActionSwitchPane, keys.ActionLabelJump,
		keys.ActionRepeat, keys.ActionShell, keys.ActionInspect, keys.ActionJumpBack, keys.ActionJumpForward,
		keys.ActionOpenFile, keys.ActionDefinition, keys.ActionReferences, keys.ActionCalls, keys.ActionMessages,
		keys.ActionPageDown, keys.ActionPageUp, keys.ActionFilter,
	)

	for i := range out {
		switch out[i].Action {
		case keys.ActionRead, keys.NextAction(keys.ObjUnread), keys.PrevAction(keys.ObjUnread),
			keys.ActionFoldToggle:
			out[i].Contexts = keys.List
		}
	}

	return out
}

// familyList is the list screens' own keys.
func familyList() []keys.Binding {
	own := func(action, what string, seqs ...string) keys.Binding {
		return keys.Binding{Action: action, Keys: seqs, What: what, Group: "Queue", Contexts: keys.List}
	}

	out := make([]keys.Binding, 0, listOwn+len(keys.Verbs()))
	out = append(out,
		own(actChoose, "open the row", enterKey),
		own(actBrowse, "on GitHub", "o"),
		own(actResolve, "resolve", "R"),
		own(actCheckout, "check out", "C"),
		own(actDiscard, "discard", "d"),
		own(actMerge, "merge", "M"),
		own(actLabels, "labels", "l"),
		// f narrows the queue to one repository, the list's own filter.
		own(keys.ActionFilter, "focus one repository", "f"),
		own(actUnfocus, "every repository", "F"),
		own(actScope, "work, personal, all", "w"),
		own(actOrder, "the next row order", "s"),
		own(actTabPick, "the queue by its number", "1", "2", "3"),
		own(actSection, "next group", tabKey),
		// P is the publish page; the queue's only publish is approving, which
		// asks again before it sends.
		own(actApprove, "approve, after a confirm", "P a"),
		// Clean is a command rather than a key: g is where things are, not verbs.
		own(actClean, "clean finished reviews"),
		own(actStale, "clean stale reviews too"),
	)

	verbs := keys.Without(keys.Verbs(),
		keys.ActionPublish, keys.ActionBranches, keys.ActionBranchDelete, keys.ActionInfo)
	for i := range verbs {
		verbs[i].Contexts = keys.List
		if verbs[i].Action == keys.ActionInsert {
			verbs[i].Short = "reply or comment"
		}
	}

	return append(out, verbs...)
}

// listMoves are this release's moved list keys.
func listMoves() keys.Moves {
	return keys.Moves{
		"m":   keys.ActionInsert,
		"r":   keys.ActionInsert,
		"A":   actApprove,
		"u":   keys.NextAction(keys.ObjUnread),
		"U":   keys.PrevAction(keys.ObjUnread),
		ctrlR: keys.ActionReload,
		"y":   keys.ActionScrollUp,
	}
}

// movedIn is a moves table's bindings on screen ctx alone.
func movedIn(m keys.Moves, ctx keys.Context) []keys.Binding {
	out := m.Bindings()
	for i := range out {
		out[i].Contexts = ctx
	}

	return out
}

// reviewKeymap is the review screen's keys merged with the family's, which a
// moved key's notice is read from.
func reviewKeymap() (*keys.Keymap, error) {
	//nolint:wrapcheck // Merge's errors already name the keys involved
	return keys.Merge(sharedReview(), slices.Concat(familyReview(), movedIn(reviewMoves(), keys.View)), nil)
}

// listKeymap is the list screens' keys merged with the family's.
func listKeymap() (*keys.Keymap, error) {
	//nolint:wrapcheck // Merge's errors already name the keys involved
	return keys.Merge(sharedList(), slices.Concat(familyList(), movedIn(listMoves(), keys.List)), nil)
}

// moveNotice is what a moved key says when pressed, false when k did not move.
// A keymap that failed to merge says nothing, which the family test catches.
func moveNotice(km *keys.Keymap, k string) (keys.Move, bool) {
	if km == nil {
		return keys.Move{}, false
	}

	return km.Move(keys.MovedAction(k))
}

// mustKeymap is build's keymap, nil where it fails to merge: a screen without
// one still works and only loses the moved keys' notices, and
// TestKeymap_AgreesWithShared fails the build that broke it.
func mustKeymap(build func() (*keys.Keymap, error)) *keys.Keymap {
	km, err := build()
	if err != nil {
		return nil
	}

	return km
}
