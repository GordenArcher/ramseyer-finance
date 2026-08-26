//go:build !windows

package main

import "os/exec"

func newDetachedCommand(path string) *exec.Cmd {
	return exec.Command(path)
}
