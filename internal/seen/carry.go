package seen

import "github.com/kyleking/second-look/internal/diff"

// Carry reports how many of a new head's hunks were already read.
//
// There is nothing to move. A mark is stored against a hunk's content, so a
// hunk the author did not touch answers the same identity under the new head
// and is still read, while one whose text changed is a different hunk and comes
// back unread. That is the answer wanted in both directions.
//
// The intended mechanism was git range-diff, and it was measured out. It
// reports per commit, and calls a pair identical only when the patch matches
// byte for
// byte, context included, which is exactly when the cumulative diff's hunks are
// byte-identical too, which is when identity already carries them. Applying its
// verdict per hunk instead of per commit would mean attributing a
// cumulative-diff hunk to one commit, which is blame-level work with its own
// failure modes. See NEXT_STEPS.md for the experiment.
func Carry(set *Set, current *diff.Diff) int {
	return set.Count(Hunks(current))
}
