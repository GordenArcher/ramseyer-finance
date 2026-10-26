package cli

import "fmt"

// windowsStartupScript builds the per-user Startup entry. Calling the app's own start
// command keeps login identical to a manual launch: it checks health first, so an already
// running server is not duplicated and the browser is not opened on every sign-in.
func windowsStartupScript(executable string) string {
	return fmt.Sprintf("@echo off\r\nstart \"\" \"%s\" start\r\n", escapeBatchValue(executable))
}
