package main

import (
	"os"
	"path/filepath"
)

// The launcher is compiled with the Windows GUI subsystem and lives at the root of the
// distributed folder. Users can therefore double-click one clearly named executable while
// the application and updater remain together under app/. No batch file or command prompt
// participates in normal startup.
func main() {
	executablePath, err := os.Executable()
	if err != nil {
		return
	}
	appPath := filepath.Join(filepath.Dir(executablePath), "app", "ramseyer-finance.exe")
	command := newApplicationCommand(appPath)
	command.Dir = filepath.Dir(appPath)
	_ = command.Start()
}
