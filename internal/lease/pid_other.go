//go:build !unix && !windows

package lease

// A platform with no liveness check cannot tell a stale claim from a live
// one, so it believes the record: reclaiming a live session's lease is the
// worse mistake of the two.
func alive(int) bool { return true }
