//go:build unix

package lease

import (
	"errors"
	"syscall"
)

// alive reports whether pid names a running process. EPERM still counts: a
// process that refuses the signal is a process that exists.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}
