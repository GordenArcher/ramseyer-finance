package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// OpenLocalPDF opens only an existing PDF in the operating system's default viewer. The
// narrow extension and regular-file checks keep the webview bridge from becoming a general
// command launcher while still giving operators a dependable path from Save PDF to Print.
func OpenLocalPDF(target string) error {
	target = strings.TrimSpace(target)
	if target == "" || !strings.EqualFold(filepath.Ext(target), ".pdf") {
		return fmt.Errorf("a PDF file is required")
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("open saved PDF: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("saved PDF path is not a regular file")
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "linux":
		return exec.Command("xdg-open", target).Start()
	default:
		return fmt.Errorf("opening PDF files is not supported on %s", runtime.GOOS)
	}
}
