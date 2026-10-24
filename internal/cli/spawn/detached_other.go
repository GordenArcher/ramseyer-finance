//go:build !windows

package spawn

import "syscall"

// detachedSysProcAttr needs no extra attributes on POSIX because the child has no terminal
// streams, so closing the launching shell does not send it a hangup.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return nil
}
