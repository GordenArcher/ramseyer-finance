//go:build !windows

package console

// Attach is a no-op outside Windows because those shells already provide the process with a
// console, so output does not need to be reconnected to a parent terminal.
func Attach() {}
