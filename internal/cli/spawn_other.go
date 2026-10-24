//go:build !windows

package cli

import "syscall"

// detachedSysProcAttr has no extra attributes on POSIX because the process is detached
// by not attaching to the controlling terminal and letting the parent exit.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return nil
}
