//go:build windows

package main

import "golang.org/x/sys/windows"

// isProcessRunning queries the Windows process handle directly. The previous tasklist.exe
// subprocess could briefly create a console during an otherwise silent GUI update; using
// the operating-system API removes that final terminal flash.
func isProcessRunning(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, err
	}
	return exitCode == 259, nil // STILL_ACTIVE
}
