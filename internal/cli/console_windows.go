//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

const attachParentProcess = 0xFFFFFFFF

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

// AttachConsole reconnects the standard streams to the console that started this process
// so CLI subcommands print their output. The packaged build is GUI subsystem to avoid a
// console window at logon, so without attaching output is discarded entirely.
func AttachConsole() {
	if rc, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); rc == 0 {
		return
	}
	console, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return
	}
	os.Stdout = console
	os.Stderr = console
}
