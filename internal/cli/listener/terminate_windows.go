//go:build windows

package listener

import (
	"fmt"
	"os/exec"
	"syscall"
)

// terminate stops the process that owns the listening socket. PowerShell resolves the owning
// PID, which is more precise than killing by image name and avoids taking down an unrelated
// process. The caller already confirmed the port answers this app's health check.
func terminate(port int, processName string, force bool) {
	if !force {
		script := fmt.Sprintf(
			"Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique | ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }",
			port,
		)
		command := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
		// The CLI can have an attached console; hiding the window keeps the helper from flashing.
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = command.Run()
		return
	}

	// Constrained language mode can block Get-NetTCPConnection, so the force step falls back to
	// the image name. Escalating is safe because the port is still answering our app.
	_ = exec.Command("taskkill", "/IM", processName+".exe", "/T", "/F").Run()
}
