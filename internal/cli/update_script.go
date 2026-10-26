package cli

import (
	"fmt"
	"strings"
)

const (
	windowsUpdateStartupPings = 3
	windowsUpdateRetryPings   = 2
	windowsUpdateMaxAttempts  = 30
)

// windowsUpdateScript builds the detached batch file that applies a staged Windows update.
// The parent process has to exit before the move because Windows locks the running executable.
// I keep the commands in a script instead of passing them to cmd.exe as one quoted string,
// because cmd has its own quoting rules and exec.Command needs nested quotes that can turn a
// path into a stray backslash argument on some installations.
func windowsUpdateScript(current, replacement, stage string) string {
	return fmt.Sprintf(`@echo off
setlocal
set "target=%s"
set "source=%s"
set "staging=%s"
set /a attempts=0

rem Wait for the updater process to exit and release the executable lock.
ping 127.0.0.1 -n %d >nul

:replace
move /Y "%%source%%" "%%target%%" >nul 2>&1
if not errorlevel 1 goto replaced
set /a attempts+=1
if %%attempts%% GEQ %d goto failed
ping 127.0.0.1 -n %d >nul
goto replace

:replaced
start "" "%%target%%"
rmdir /S /Q "%%staging%%" >nul 2>&1
exit /b 0

:failed
start "" "%%target%%"
exit /b 1
`, escapeBatchValue(current), escapeBatchValue(replacement), escapeBatchValue(stage), windowsUpdateStartupPings, windowsUpdateMaxAttempts, windowsUpdateRetryPings)
}

// escapeBatchValue doubles percent signs because cmd expands them even inside quoted strings.
// A username or folder containing a percent sign would otherwise be read as an environment
// variable and silently change the destination path.
func escapeBatchValue(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}
