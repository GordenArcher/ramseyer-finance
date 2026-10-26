package cli

import (
	"strings"
	"testing"
)

func TestWindowsStartupScriptQuotesExecutable(t *testing.T) {
	script := windowsStartupScript(`C:\Program Files\Ramseyer Finance\ramseyer-finance.exe`)

	want := `start "" "C:\Program Files\Ramseyer Finance\ramseyer-finance.exe" start`
	if !strings.Contains(script, want) {
		t.Fatalf("startup script does not contain %q:\n%s", want, script)
	}
}

func TestWindowsStartupScriptEscapesPercentInExecutable(t *testing.T) {
	script := windowsStartupScript(`C:\Users\100%\ramseyer-finance.exe`)

	want := `start "" "C:\Users\100%%\ramseyer-finance.exe" start`
	if !strings.Contains(script, want) {
		t.Fatalf("startup script did not escape the percent sign:\n%s", script)
	}
}
