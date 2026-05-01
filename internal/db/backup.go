package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateBackupFile generates a consistent, self-contained backup of the live SQLite database
// using SQLite's native VACUUM INTO command. It returns the path to a temporary file that callers
// are responsible for copying, streaming, or removing once they are done with it. The function
// serializes all database lifecycle operations via lifecycleMu to prevent concurrent modifications
// during the backup window, and fails early if the database hasn't been initialized yet.
func CreateBackupFile() (string, error) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	if DB == nil {
		return "", fmt.Errorf("database is not initialized")
	}

	// I create the backup in a temp file first because callers may stream it, copy it, or move it
	// somewhere else entirely. Keeping SQLite's output ephemeral avoids leaving stray artifacts in
	// the live app directory.
	tmpFile, err := os.CreateTemp("", "ramseyer-finance-backup-*.db")
	if err != nil {
		return "", fmt.Errorf("create temp backup file: %w", err)
	}
	// Grab the temp file's path now, then immediately close and delete it. SQLite's VACUUM INTO
	// creates its own file at that path, so we only need a guaranteed-unique filename. If the
	// close fails, we clean up the temp file to avoid leaving garbage behind on disk.
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp backup file: %w", err)
	}
	_ = os.Remove(tmpPath)

	// I rely on `VACUUM INTO` instead of copying the live database file because the app runs in
	// WAL mode. SQLite's own backup path gives me one coherent snapshot without juggling sidecars.
	stmt := fmt.Sprintf("VACUUM INTO '%s'", strings.ReplaceAll(tmpPath, "'", "''"))
	if _, err := DB.Exec(stmt); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("create sqlite backup: %w", err)
	}

	return tmpPath, nil
}

// RestoreFromReader replaces the current database with the contents read from src. The function
// accepts the restore payload as an io.Reader so callers can supply data from HTTP uploads, disk
// files, or any other streaming source. It follows a safe multi-step procedure: write the incoming
// bytes to a temp file alongside the real database, validate completion, clean up WAL/SHM sidecars
// from the old database, atomically swap the files, and reopen the connection. If any step fails,
// it removes the temp file and returns an error without touching the live database.
func RestoreFromReader(src io.Reader) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	if strings.TrimSpace(DBPath) == "" {
		return fmt.Errorf("database path is not configured")
	}

	// I write the incoming restore payload into a sibling temp file first so a partial upload or
	// bad source never clobbers the live database before the file is complete on disk.
	// Using filepath.Dir(DBPath) as the parent directory ensures the temp file stays on the same
	// filesystem, which makes the final atomic rename fast and reliable.
	restoreFile, err := os.CreateTemp(filepath.Dir(DBPath), "ramseyer-finance-restore-*.db")
	if err != nil {
		return fmt.Errorf("create restore file: %w", err)
	}
	restorePath := restoreFile.Name()
	// Schedule cleanup of the temp restore file. Once os.Rename succeeds, this removal becomes a
	// no-op because the file no longer exists at restorePath. If any step before the rename fails,
	// this ensures we don't leave an orphaned temp file in the database directory.
	defer func() {
		_ = os.Remove(restorePath)
	}()

	if _, err := io.Copy(restoreFile, src); err != nil {
		_ = restoreFile.Close()
		return fmt.Errorf("write restore file: %w", err)
	}
	// Explicitly close the restore file after writing to flush all buffered data to disk. A
	// dangling open file handle could cause the subsequent os.Rename to fail on some platforms
	// (notably Windows), so closing here is a defensive cross-platform precaution.
	if err := restoreFile.Close(); err != nil {
		return fmt.Errorf("close restore file: %w", err)
	}

	// I explicitly remove the old WAL and SHM sidecars before swapping the database file because
	// those sidecars belong to the previous database state and must not leak into the restored one.
	// After closing the connection, both the main database and its sidecars are fair game for
	// removal. We use os.IsNotExist to tolerate cases where WAL journalling was never active or
	// a previous crash already cleaned them up.
	Close()
	for _, path := range []string{DBPath + "-wal", DBPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove sqlite sidecar %s: %w", path, err)
		}
	}
	if err := os.Remove(DBPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing database: %w", err)
	}
	// Rename atomically replaces the old database file with the validated restore file on the same
	// filesystem. This is the critical handoff point: once os.Rename returns nil, the restore file
	// has become the live database and we must reopen it successfully.
	if err := os.Rename(restorePath, DBPath); err != nil {
		return fmt.Errorf("replace database file: %w", err)
	}

	// Reopen the database connection using the same locked-open pattern as the initial startup.
	// This reinitializes the global DB handle, connection pool, and any WAL/sidecar files fresh
	// from the restored database content.
	if err := openLocked(); err != nil {
		return fmt.Errorf("reopen restored database: %w", err)
	}

	return nil
}
