//go:build !windows

package cli

// AttachConsole is a no-op outside Windows because those shells already provide a console
// to the process and output does not need to be reattached.
func AttachConsole() {}
