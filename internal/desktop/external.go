package desktop

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// OpenExternalURL launches a trusted application link in the user's default browser. This
// is the only remaining desktop-shell bridge: backup selection now lives entirely inside
// the web UI, so this package does not expose filesystem dialogs or return local paths.
func OpenExternalURL(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("external URL is required")
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "linux":
		return exec.Command("xdg-open", target).Start()
	default:
		return fmt.Errorf("external URL opening is not supported on %s", runtime.GOOS)
	}
}
