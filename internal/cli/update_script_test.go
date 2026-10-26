package cli

import (
	"strings"
	"testing"
)

func TestWindowsUpdateScriptQuotesUpdatePaths(t *testing.T) {
	script := windowsUpdateScript(
		`C:\Program Files\Ramseyer Finance\ramseyer-finance.exe`,
		`C:\Users\Test User\AppData\Local\Temp\ramseyer-finance-update-123\ramseyer-finance.exe`,
		`C:\Users\Test User\AppData\Local\Temp\ramseyer-finance-update-123`,
	)

	wanted := []string{
		`set "target=C:\Program Files\Ramseyer Finance\ramseyer-finance.exe"`,
		`set "source=C:\Users\Test User\AppData\Local\Temp\ramseyer-finance-update-123\ramseyer-finance.exe"`,
		`set "staging=C:\Users\Test User\AppData\Local\Temp\ramseyer-finance-update-123"`,
		`move /Y "%source%" "%target%"`,
		`start "" "%target%"`,
		`rmdir /S /Q "%staging%"`,
	}
	for _, want := range wanted {
		if !strings.Contains(script, want) {
			t.Fatalf("script does not contain %q:\n%s", want, script)
		}
	}
}

func TestWindowsUpdateScriptEscapesPercentInPaths(t *testing.T) {
	script := windowsUpdateScript(
		`C:\Users\100%\ramseyer-finance.exe`,
		`C:\Temp\source\ramseyer-finance.exe`,
		`C:\Temp\source`,
	)

	if !strings.Contains(script, `set "target=C:\Users\100%%\ramseyer-finance.exe"`) {
		t.Fatalf("target percent sign was not escaped:\n%s", script)
	}
}

func TestWindowsUpdateScriptRetriesBeforeLaunchingOldBinary(t *testing.T) {
	script := windowsUpdateScript(`C:\current.exe`, `C:\source.exe`, `C:\stage`)

	if !strings.Contains(script, "set /a attempts+=1") {
		t.Fatalf("script does not count replacement attempts:\n%s", script)
	}
	if !strings.Contains(script, "if %attempts% GEQ 30 goto failed") {
		t.Fatalf("script does not cap replacement attempts:\n%s", script)
	}
	if !strings.Contains(script, `start "" "%target%"`) {
		t.Fatalf("script does not relaunch the installed binary:\n%s", script)
	}
}
