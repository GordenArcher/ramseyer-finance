package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// savedFileResponse is the JSON payload returned to the desktop webview after a native
// file save operation (backup download or transaction export). It includes the absolute
// filesystem path where the file was written, the final filename (which may differ from
// the requested name if a numeric suffix was added to avoid overwriting), and a
// human-readable status message for display in the UI. This struct is used by both the
// backup download and export handlers when the "delivery=native" query parameter is set.
type savedFileResponse struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Message  string `json:"message"`
}

// resolveUserDownloadsDir locates (or creates) the user's Downloads directory for saving
// exported files and native backup downloads. It first attempts the standard cross-platform
// home-directory lookup via os.UserHomeDir, appending "Downloads" to that path. If the home
// directory is unavailable (e.g., in a minimal container or a misconfigured environment),
// it falls back to a "downloads" subdirectory within the current working directory. Both
// paths are created with MkdirAll so the function succeeds even on a fresh system where
// the Downloads folder doesn't yet exist. The directory is created with 0755 permissions
// (owner read/write/execute, group and others read/execute).
func resolveUserDownloadsDir() (string, error) {
	// I default to the user's Downloads directory because it is the most discoverable destination
	// for saved exports and backups in a desktop workflow. The fallback only exists for edge cases.
	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		downloadsDir := filepath.Join(homeDir, "Downloads")
		if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
			return "", fmt.Errorf("create downloads directory: %w", err)
		}
		return downloadsDir, nil
	}

	// Fallback path: when there's no home directory (rare on desktop, but possible in
	// sandboxed or headless environments), use a "downloads" folder relative to the
	// current working directory. This ensures the export feature doesn't fail entirely,
	// even if the save location is less ideal.
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory fallback: %w", err)
	}

	downloadsDir := filepath.Join(workingDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return "", fmt.Errorf("create fallback downloads directory: %w", err)
	}
	return downloadsDir, nil
}

// resolveBackupRootDir determines the root directory under which all backup-related
// folders (Auto, Safety, and any future backup types) are created. It first checks for
// the RAMSEYER_FINANCE_BACKUP_DIR environment variable, which allows deployments and
// power users to redirect backups to a custom location (e.g., a network drive, a
// cloud-synced folder, or a dedicated backup volume). If the environment variable is not
// set or is empty, it falls back to the user's Downloads directory—the same location
// used for manual exports—keeping all saved files in one discoverable place by default.
func resolveBackupRootDir() (string, error) {
	// I allow a backup root override for deployments that want managed storage, but the default
	// path should remain obvious for ordinary local users.
	if customDir := strings.TrimSpace(os.Getenv("RAMSEYER_FINANCE_BACKUP_DIR")); customDir != "" {
		if err := os.MkdirAll(customDir, 0o755); err != nil {
			return "", fmt.Errorf("create custom backup directory: %w", err)
		}
		return customDir, nil
	}

	return resolveUserDownloadsDir()
}

// nextAvailableFilePath returns a filesystem path that is guaranteed not to already exist,
// based on a parent directory and a desired filename. If the base filename is available,
// it is returned unchanged. If a file with that name already exists, the function appends
// a numeric suffix (e.g., "report.csv" becomes "report-1.csv", then "report-2.csv", etc.)
// until it finds an unused name. This avoids silently overwriting previous exports or
// backups, which is especially important because these files serve as durable records.
// The function scans sequentially from 1 upward with no upper limit, which is safe because
// the number of colliding files in normal use is small.
func nextAvailableFilePath(dir, filename string) string {
	// I never overwrite an existing export or backup by default because these files are records.
	// Name collisions should produce numbered siblings, not silent replacement.
	extension := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, extension)
	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	// Linear probing: try base-1.ext, base-2.ext, base-3.ext, and so on until we find
	// a name that doesn't collide. The hyphen separator is chosen over parentheses (like
	// "file (2).ext") to avoid issues with shell escaping and URL encoding.
	for index := 1; ; index++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, index, extension))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// writeJSON serialises the given payload as JSON and writes it to the HTTP response with
// the appropriate Content-Type header and status code. This is a small helper that avoids
// repeating the header setup and encoding boilerplate across the several handlers that
// return JSON responses (native backup save, native export save, and any future API
// endpoints). The function does not handle encoding errors explicitly—if JSON encoding
// fails (which should be rare for simple structs), the error is silently dropped because
// at that point the status code and headers have already been written and cannot be
// changed.
func writeJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

// copyFile performs a byte-for-byte copy from a source file to a destination file,
// creating the destination if it doesn't exist or truncating it if it does. After the
// copy completes, it calls Sync on the destination file to flush the operating system's
// write buffers to disk. This fsync is an intentional trade-off: it adds a small
// performance cost compared to a plain copy, but it guarantees that when this function
// returns successfully, the file data has been handed off to the storage layer. For
// backup and export operations where the caller may immediately report "file saved" to
// the user, this durability guarantee matters more than marginal speed.
func copyFile(sourcePath, destinationPath string) error {
	// I finish with an fsync so a reported "saved" file has actually been flushed to disk as far
	// as the OS allows. That matters more here than a tiny speed gain.
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}

	// Sync flushes the file's in-memory buffers to the underlying storage device. On
	// most operating systems this translates to an fsync system call. Without this, a
	// system crash or power loss shortly after the copy could result in a zero-byte or
	// truncated destination file despite the function returning nil.
	return destinationFile.Sync()
}
