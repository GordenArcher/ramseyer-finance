package spawn

import "os/exec"

// Detached starts executable without inheriting the current terminal. The child has to outlive
// the command that launched it, so Start returns as soon as the process exists and the server
// keeps running after the shell closes.
func Detached(executable string, args ...string) error {
	command := exec.Command(executable, args...)
	command.SysProcAttr = detachedSysProcAttr()
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	return command.Start()
}
