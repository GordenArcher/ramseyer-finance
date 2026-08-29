// Package diskspace provides the small cross-platform boundary used before the app writes
// a recovery database. Keeping the operating-system calls here prevents backup behavior
// from being coupled to platform-specific syscall details.
package diskspace

import "fmt"

// EnsureAvailable verifies that the filesystem containing path has enough space for the
// requested write. Callers still need to handle write-time failures because another process
// can consume space after this check, but failing early gives the user a useful explanation
// before SQLite begins a potentially expensive backup operation.
func EnsureAvailable(path string, required uint64) error {
	available, err := availableBytes(path)
	if err != nil {
		return fmt.Errorf("check available storage: %w", err)
	}
	if available < required {
		return fmt.Errorf(
			"not enough free storage: need %s but only %s is available",
			formatBytes(required),
			formatBytes(available),
		)
	}
	return nil
}

func formatBytes(value uint64) string {
	const (
		megabyte = 1024 * 1024
		gigabyte = 1024 * megabyte
	)
	if value >= gigabyte {
		return fmt.Sprintf("%.1f GB", float64(value)/gigabyte)
	}
	return fmt.Sprintf("%.1f MB", float64(value)/megabyte)
}
