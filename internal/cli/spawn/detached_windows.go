//go:build windows

package spawn

import "syscall"

const detachedProcessCreationFlags = 0x00000008

// detachedSysProcAttr hides the console and detaches the child so the fallback server can
// outlive the terminal that launched it.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcessCreationFlags,
	}
}
