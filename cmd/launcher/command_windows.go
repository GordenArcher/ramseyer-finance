//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// newApplicationCommand provides a second terminal-free guard in addition to both
// executables being compiled for the Windows GUI subsystem. This keeps startup silent even
// if the child executable is replaced during an update before the launcher is refreshed.
func newApplicationCommand(path string) *exec.Cmd {
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
