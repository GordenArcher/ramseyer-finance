//go:build !windows

package main

import "os/exec"

// The launcher is distributed only on Windows; this fallback keeps repository-wide tests
// and editor tooling portable on development machines.
func newApplicationCommand(path string) *exec.Cmd {
	return exec.Command(path)
}
