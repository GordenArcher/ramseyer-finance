//go:build !windows

package main

// The updater exits before this helper is reached on non-Windows systems. The stub keeps
// cross-platform tests and editor tooling able to compile the package.
func isProcessRunning(pid int) (bool, error) {
	return false, nil
}
