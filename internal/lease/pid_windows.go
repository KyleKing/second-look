package lease

import "golang.org/x/sys/windows"

// alive reports whether pid names a running process. A handle that opens is
// not proof on its own: Windows lets a dead process's handle outlive it, so
// the exit code is what answers.
func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}

	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}

	return code == 259 // STILL_ACTIVE, not exported by x/sys
}
