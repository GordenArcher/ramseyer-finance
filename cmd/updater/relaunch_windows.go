//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// newDetachedCommand starts the GUI launcher without inheriting or creating a terminal.
func newDetachedCommand(path string) *exec.Cmd {
	const (
		createNewProcessGroup = 0x00000200
		createNoWindow        = 0x08000000
	)
	command := exec.Command(path)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNewProcessGroup | createNoWindow,
	}
	return command
}
