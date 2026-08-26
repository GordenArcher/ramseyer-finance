package backup

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/webui"
	"strings"
	"time"
)

// BackupData carries the template variables for the backup management page. Active
// identifies the current navigation tab. Message and MessageTone provide feedback after
// redirects (e.g., "Restore successful" or "No backup file selected"). The History slice
// contains the most recent backup events—both manual and automatic—for display in the
// activity log on the page.
type BackupData struct {
	Active            string
	Message           string
	MessageTone       string
	History           []BackupHistoryEntry
	RestoreCandidates []RestoreCandidate
}

// BackupPage serves the backup management interface. It loads the most recent 20 backup
// history entries, parses any feedback message from the query string, and renders the
// full backup template. The page includes controls for manual backup download, backup
// restore, and a log of recent backup activity.
func BackupPage(w http.ResponseWriter, r *http.Request) {
	history, err := LoadBackupHistory(20)
	if err != nil {
		serverError(w, err)
		return
	}
	restoreCandidates, err := LoadRestoreCandidates()
	if err != nil {
		serverError(w, err)
		return
	}

	data := BackupData{
		Active:            "backup",
		Message:           r.URL.Query().Get("msg"),
		MessageTone:       alertTone(r.URL.Query().Get("msg")),
		History:           history,
		RestoreCandidates: restoreCandidates,
	}
	webui.RenderTemplate(w, "backup", data)
}

// DownloadBackup handles backup file download requests. It supports two delivery modes
// selected by the "delivery" query parameter: the default browser-based download (which
// streams the file as an attachment with Content-Disposition headers), and a "native"
// mode designed for embedded desktop webviews that save the file directly to the user's
// downloads folder and return a JSON response with the saved path. Both modes go through
// db.CreateBackupFile to get a consistent, WAL-safe SQLite snapshot.
func DownloadBackup(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.URL.Query().Get("delivery")) == "native" {
		// I support a native-save path for the desktop shell because some embedded webviews do not
		// behave like a full browser download manager. Returning JSON lets the frontend show the
		// saved location instead of navigating away to raw file contents.
		saveBackupLocally(w)
		return
	}

	// I keep the attachment response for normal browser use so this endpoint still behaves
	// correctly outside the desktop wrapper.
	backupPath, err := db.CreateBackupFile()
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() {
		_ = os.Remove(backupPath)
	}()

	// Set Content-Disposition to "attachment" so the browser prompts the user to save the
	// file rather than displaying raw binary content in the window. The filename includes
	// the current date for easy identification in the user's downloads folder.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=ramseyer-finance-backup-"+time.Now().Format("2006-01-02")+".db")
	http.ServeFile(w, r, backupPath)
}

// RestoreBackup handles backup restoration requests. It accepts either a file upload from
// a browser form or a native filesystem path from the desktop webview, validates the source
// before touching the live database, creates a safety backup of the current database state
// in case the restore goes wrong, delegates the actual restore to the database layer (which
// handles WAL cleanup and atomic file replacement), logs the outcome to the backup event
// history, and redirects back to the backup page with a status message.
func RestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// I resolve the restore source before touching the live database so a bad picker result or
	// broken upload fails fast without creating side effects.
	restoreReader, restoreLabel, cleanup, err := openRestoreSource(r)
	if err != nil {
		http.Redirect(w, r, "/backup?msg="+queryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	defer cleanup()

	// I create a safety snapshot before every restore because restore is the most destructive
	// action in the app. If the wrong file is chosen, I still have a rollback point.
	safetyBackupPath, err := createSafetyBackup(time.Now())
	if err != nil {
		_ = RecordBackupEvent("pre-restore-safety", "failed", "", err.Error())
		serverError(w, fmt.Errorf("create safety backup: %w", err))
		return
	}
	_ = RecordBackupEvent("pre-restore-safety", "success", safetyBackupPath, "Safety backup created before restore")

	// I route restore through the database layer instead of swapping files inside the handler
	// because SQLite WAL mode needs a controlled restore path, not a blind overwrite.
	if err := db.RestoreFromReader(restoreReader); err != nil {
		_ = RecordBackupEvent("restore", "failed", restoreLabel, err.Error())
		serverError(w, err)
		return
	}
	_ = RecordBackupEvent("restore", "success", restoreLabel, "Backup restore completed")

	http.Redirect(w, r, "/backup?msg=Restore+successful", http.StatusSeeOther)
}

// saveBackupLocally writes the current database backup directly to the user's downloads
// directory, bypassing the browser's download mechanism entirely. This is designed for
// embedded desktop webview environments where Content-Disposition headers may be ignored
// or where the application wants direct control over the save location. On success, it
// returns a JSON response containing the saved file's path and filename so the frontend
// can display the result to the user. If the target filename already exists, it appends
// a numeric suffix (e.g., "file (2).db") to avoid overwriting previous backups.
func saveBackupLocally(w http.ResponseWriter) {
	// I save into the user's downloads area by default because that is the most discoverable
	// location in a desktop flow where there is no browser download shelf.
	backupPath, err := db.CreateBackupFile()
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() {
		_ = os.Remove(backupPath)
	}()

	downloadsDir, err := ResolveUserDownloadsDir()
	if err != nil {
		serverError(w, err)
		return
	}

	filename := "ramseyer-finance-backup-" + time.Now().Format("2006-01-02") + ".db"
	// Use nextAvailableFilePath to avoid clobbering existing backups with the same date.
	// This generates names like "ramseyer-finance-backup-2026-05-01 (2).db" if the base
	// name is already taken.
	targetPath := NextAvailableFilePath(downloadsDir, filename)
	if err := CopyFile(backupPath, targetPath); err != nil {
		_ = RecordBackupEvent("manual-download", "failed", targetPath, err.Error())
		serverError(w, fmt.Errorf("save backup to downloads: %w", err))
		return
	}
	_ = RecordBackupEvent("manual-download", "success", targetPath, "Manual backup download")

	WriteJSON(w, http.StatusOK, SavedFileResponse{
		Path:     targetPath,
		Filename: filepath.Base(targetPath),
		Message:  "Backup saved",
	})
}

// openRestoreSource inspects the incoming HTTP request and returns an io.ReadCloser for
// the restore payload. It supports two input methods: a managed filesystem path selected
// inside the custom backup library and a multipart file supplied through drag-and-drop.
// The function also returns a
// human-readable label for logging (the file path or the uploaded filename), and a cleanup
// function that callers must defer to close the underlying file handle. If neither input
// method provides a valid source, it returns a descriptive error.
func openRestoreSource(r *http.Request) (io.ReadCloser, string, func(), error) {
	// I validate custom-picker paths again on the server because a hidden form value can be
	// changed with developer tools. Without this boundary, the restore endpoint could be
	// tricked into reading an arbitrary local file rather than a managed SQLite backup.
	backupPath := strings.TrimSpace(r.FormValue("backup_path"))
	if backupPath != "" {
		validatedPath, err := ValidateManagedRestorePath(backupPath)
		if err != nil {
			return nil, "", func() {}, err
		}
		file, err := os.Open(validatedPath)
		if err != nil {
			return nil, "", func() {}, fmt.Errorf("Selected backup file could not be opened")
		}
		return file, validatedPath, func() {
			_ = file.Close()
		}, nil
	}

	file, header, err := r.FormFile("backup_file")
	if err != nil {
		return nil, "", func() {}, fmt.Errorf("No backup file selected")
	}
	return file, header.Filename, func() {
		_ = file.Close()
	}, nil
}

// createSafetyBackup creates a timestamped backup of the current database state in a
// dedicated "Safety" subdirectory. This backup is intended as a rollback point before a
// restore operation—if the restore file turns out to be wrong, the user (or support) can
// manually restore from the safety backup. The function uses the same db.CreateBackupFile
// path as all other backup operations, ensuring WAL consistency, and names the file with
// a "pre-restore" prefix and a sortable timestamp for easy identification.
func createSafetyBackup(now time.Time) (string, error) {
	// I create a pre-restore backup every time because restore is destructive by definition.
	// The safety copy gives me a rollback point even if the selected file is wrong.
	backupPath, err := db.CreateBackupFile()
	if err != nil {
		return "", fmt.Errorf("create sqlite safety backup: %w", err)
	}
	defer func() {
		_ = os.Remove(backupPath)
	}()

	rootDir, err := ResolveBackupRootDir()
	if err != nil {
		return "", fmt.Errorf("resolve backup root directory: %w", err)
	}

	// Safety backups live in their own directory to keep them separate from automatic
	// backups and manual downloads. This makes it obvious which files are rollback
	// points rather than routine snapshots.
	safetyDir := filepath.Join(rootDir, "Ramseyer Finance Backups", "Safety")
	if err := os.MkdirAll(safetyDir, 0o755); err != nil {
		return "", fmt.Errorf("create safety backup directory: %w", err)
	}

	filename := "ramseyer-finance-pre-restore-" + now.Format("2006-01-02-150405") + ".db"
	targetPath := filepath.Join(safetyDir, filename)
	if err := CopyFile(backupPath, targetPath); err != nil {
		return "", fmt.Errorf("save safety backup: %w", err)
	}

	return targetPath, nil
}
