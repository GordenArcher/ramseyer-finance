//go:build windows

package console

import (
	"os"

	"golang.org/x/sys/windows"
)

const attachParentProcess = 0xFFFFFFFF

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

// Attach reconnects the standard streams to the console that started the process. The packaged
// build is GUI subsystem so logon stays silent, which means output is discarded unless we
// explicitly attach. Without this every status, health, and update message is invisible.
func Attach() {
	if rc, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); rc == 0 {
		// No parent console, for example when Task Scheduler launched the binary. Staying
		// silent is correct there and attaching must not fail the command.
		return
	}
	console, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return
	}
	os.Stdout = console
	os.Stderr = console
}
