package nativepicker

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// PickBackupFile opens a platform-native file selection dialog and returns the absolute
// path of the file chosen by the user. This function is designed for the desktop webview
// environment where the HTML <input type="file"> element may not behave reliably or may
// not provide the full filesystem path (some embedded browsers only return a sanitised
// filename). By delegating to the operating system's native dialog, the application gets
// a real, usable filesystem path that can be passed directly to the restore handler's
// "backup_path" form field.
//
// The function dispatches to OS-specific implementations based on runtime.GOOS:
//   - macOS: uses osascript to invoke the native AppleScript file chooser
//   - Windows: uses PowerShell to create a .NET OpenFileDialog
//   - Linux: tries zenity first, then kdialog as a fallback
//
// On unsupported platforms, it returns an error. This package is intentionally kept
// separate from the handlers package so that it can be imported consistently regardless
// of whether the application is started with "go run main.go" or "go run ." (Go ignores
// root-level sibling files when a specific file is named, but always compiles imported
// packages).
func PickBackupFile() (string, error) {
	// I keep the native picker in an imported package so `go run main.go` works.
	// Root-level sibling files are ignored by `go run main.go`, but imported packages
	// are compiled consistently whether the app is started with `go run main.go` or `go run .`.
	switch runtime.GOOS {
	case "darwin":
		return pickBackupFileMacOS()
	case "windows":
		return pickBackupFileWindows()
	case "linux":
		return pickBackupFileLinux()
	default:
		return "", fmt.Errorf("native backup picker is not supported on %s", runtime.GOOS)
	}
}

// pickBackupFileMacOS opens the native macOS file picker using osascript (AppleScript via
// the command line). It displays a dialog prompting the user to "Select a backup file to
// restore" and returns the chosen file's POSIX path (e.g., "/Users/name/Downloads/backup.db").
// If the user cancels the dialog (AppleScript error number -128), the script returns an
// empty string rather than propagating the error, allowing the caller to distinguish
// between a deliberate cancellation and a genuine failure. The try/on error/end try block
// is necessary because osascript exits with a non-zero status on user cancellation, which
// would otherwise cause exec.Command to return an error.
func pickBackupFileMacOS() (string, error) {
	// Each "-e" flag supplies one line of the AppleScript. The try block attempts to
	// show the file chooser; the on error block catches the user-cancelled signal
	// (error code -128) and returns an empty string instead of propagating the error.
	output, err := exec.Command(
		"osascript",
		"-e", `try`,
		"-e", `POSIX path of (choose file with prompt "Select a backup file to restore")`,
		"-e", `on error number -128`,
		"-e", `return ""`,
		"-e", `end try`,
	).Output()
	if err != nil {
		return "", fmt.Errorf("open macOS file picker: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// pickBackupFileWindows opens the native Windows file picker using PowerShell and .NET's
// System.Windows.Forms.OpenFileDialog class. The dialog is configured with a filter that
// prefers SQLite database extensions (*.db, *.sqlite, *.sqlite3) but also allows all
// files, and a title prompting the user to "Select a backup file to restore". The
// PowerShell commands are joined with semicolons into a single script and executed with
// -NoProfile for faster startup. If the user cancels the dialog, PowerShell produces no
// output and exits successfully, resulting in an empty string return value.
func pickBackupFileWindows() (string, error) {
	// Build a single PowerShell script by joining statements with semicolons. The
	// Add-Type cmdlet loads the Windows Forms assembly, the OpenFileDialog object is
	// configured with a file filter and title, ShowDialog() displays the dialog
	// modally, and the selected filename is written to stdout if the user clicks OK.
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"$dialog = New-Object System.Windows.Forms.OpenFileDialog",
		`$dialog.Filter = "SQLite Backup Files (*.db;*.sqlite;*.sqlite3)|*.db;*.sqlite;*.sqlite3|All Files (*.*)|*.*"`,
		`$dialog.Title = "Select a backup file to restore"`,
		`if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $dialog.FileName }`,
	}, "; ")
	output, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("open Windows file picker: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// pickBackupFileLinux opens a file picker dialog on Linux by trying two common desktop
// toolkit utilities in order: zenity (GTK-based, common on GNOME and Xfce desktops) and
// kdialog (Qt-based, common on KDE Plasma). It uses exec.LookPath to check whether each
// utility is installed before attempting to run it. If neither utility is found, it
// returns an error instructing the user to install one of them. Unlike the macOS picker,
// both zenity and kdialog handle user cancellation gracefully by producing no output and
// exiting with a zero status, so no special error handling is needed for cancellation.
func pickBackupFileLinux() (string, error) {
	// Try zenity first—it is the most widely available file picker utility on Linux
	// desktops. The --file-selection flag opens a file chooser dialog, and --title
	// sets the window title.
	if path, err := exec.LookPath("zenity"); err == nil {
		output, execErr := exec.Command(path, "--file-selection", "--title=Select a backup file to restore").Output()
		if execErr != nil {
			return "", fmt.Errorf("open Linux file picker with zenity: %w", execErr)
		}
		return strings.TrimSpace(string(output)), nil
	}
	// If zenity is not available, try kdialog as a fallback for KDE-based desktops.
	// The --getopenfilename flag opens a standard file open dialog.
	if path, err := exec.LookPath("kdialog"); err == nil {
		output, execErr := exec.Command(path, "--getopenfilename").Output()
		if execErr != nil {
			return "", fmt.Errorf("open Linux file picker with kdialog: %w", execErr)
		}
		return strings.TrimSpace(string(output)), nil
	}
	// Neither utility is installed. The error message tells the user which packages
	// to install to get a working file picker on their distribution.
	return "", fmt.Errorf("no supported Linux file picker found")
}
