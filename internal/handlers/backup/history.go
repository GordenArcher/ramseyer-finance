package backup

import (
	"fmt"
	"os"
	"ramseyer-finance/internal/db"
	"time"
)

// BackupHistoryEntry represents a single row from the backup_events audit log, formatted
// for display in the backup page's activity table. It includes the database record ID,
// the kind of operation (e.g., "manual-download", "auto-weekly", "restore"), the outcome
// status ("success" or "failed"), the filesystem path of the resulting backup file, a
// human-readable note, and a formatted timestamp. The FileFound boolean is computed at
// query time by checking whether the file still exists on disk, allowing the UI to show
// which historical backups are still available for restore.
type BackupHistoryEntry struct {
	ID        int64
	Kind      string
	Status    string
	FilePath  string
	Note      string
	CreatedAt string
	FileFound bool
}

// recordBackupEvent inserts a new row into the backup_events audit table. This function is
// called from every code path that creates, restores, or attempts a backup, including
// automatic backups, manual downloads, pre-restore safety snapshots, and failed operations.
// By logging both successes and failures, the history table gives the user (and support) a
// complete picture of all backup-related activity without needing to inspect log files or
// directory listings.
func RecordBackupEvent(kind, status, filePath, note string) error {
	// I keep a backup event log in SQLite because file system inspection alone cannot answer
	// whether the app created a backup, skipped one, or produced a safety copy before restore.
	if _, err := db.DB.Exec(`
		INSERT INTO backup_events (kind, status, file_path, note)
		VALUES (?, ?, ?, ?)
	`, kind, status, filePath, note); err != nil {
		return fmt.Errorf("insert backup event: %w", err)
	}
	return nil
}

// loadBackupHistory retrieves the most recent backup events from the database, limited to
// the specified number of rows. For each event, it checks whether the referenced backup
// file still exists on disk (setting FileFound accordingly) and reformats the stored
// timestamp from the database's compact format into a human-readable display format.
// The results are sorted with the most recent event first so the UI shows the latest
// activity at the top of the activity log.
func LoadBackupHistory(limit int) ([]BackupHistoryEntry, error) {
	// I read history from SQLite instead of reconstructing it from directory listings because
	// the user needs to know which action happened, not just which files exist right now.
	rows, err := db.DB.Query(`
		SELECT id, kind, status, file_path, note, created_at
		FROM backup_events
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query backup history: %w", err)
	}
	defer rows.Close()

	var history []BackupHistoryEntry
	for rows.Next() {
		var entry BackupHistoryEntry
		if err := rows.Scan(&entry.ID, &entry.Kind, &entry.Status, &entry.FilePath, &entry.Note, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan backup history: %w", err)
		}
		// Probe the filesystem for each file path in the history so the UI can show a
		// visual indicator (e.g., a green checkmark or a grey "missing" label) next to
		// each entry. Files may have been moved, renamed, or deleted by the user since
		// the backup event was recorded, and we want to reflect that honestly.
		if entry.FilePath != "" {
			// I still probe the current path because a historical event can remain valid even if the
			// file has since been moved or deleted. The flag only tells the UI whether it still exists now.
			if _, err := os.Stat(entry.FilePath); err == nil {
				entry.FileFound = true
			}
		}
		// Reformat the stored timestamp from "2006-01-02 15:04:05" (the database's
		// default datetime format) to a more readable "02 Jan 2006 15:04" display
		// format. If parsing fails for any reason, we leave the original string in
		// place rather than discarding the value entirely.
		if parsed, err := time.Parse("2006-01-02 15:04:05", entry.CreatedAt); err == nil {
			entry.CreatedAt = parsed.Format("02 Jan 2006 15:04")
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup history: %w", err)
	}
	return history, nil
}
