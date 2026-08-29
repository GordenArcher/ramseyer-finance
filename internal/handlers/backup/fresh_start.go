package backup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/diskspace"
	"strings"
	"time"
)

const (
	// FreshStartRecoveryLifetime is intentionally short because these snapshots are an
	// emergency undo window, not part of the user's long-term backup schedule.
	FreshStartRecoveryLifetime = 72 * time.Hour
	freshStartRecoveryFolder   = "Fresh Start Recovery"
	freshStartRecoveryPrefix   = "ramseyer-finance-before-fresh-start-"
	recoveryStorageReserve     = uint64(32 * 1024 * 1024)
)

var ensureAvailableStorage = diskspace.EnsureAvailable

// CreateFreshStartRecovery saves a WAL-consistent snapshot in the managed backup library
// before financial records are cleared. It checks the destination and SQLite temporary
// filesystems up front, because discovering a full disk halfway through the only recovery
// copy would make the fresh-start operation unsafe.
func CreateFreshStartRecovery(now time.Time) (string, time.Time, error) {
	recoveryDir, err := resolveFreshStartRecoveryDir()
	if err != nil {
		return "", time.Time{}, err
	}

	databaseInfo, err := os.Stat(db.DBPath)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("inspect current database size: %w", err)
	}
	estimatedSize := uint64(databaseInfo.Size())
	if estimatedSize < 1024*1024 {
		estimatedSize = 1024 * 1024
	}

	// SQLite first writes a compact temporary database and the app then copies it into the
	// managed recovery directory. Reserving roughly twice the live size plus 32 MB covers
	// both files when Temp and Downloads share a disk, while still leaving breathing room
	// for WAL activity and operating-system metadata.
	requiredSpace := estimatedSize*2 + recoveryStorageReserve
	if err := ensureAvailableStorage(recoveryDir, requiredSpace); err != nil {
		return "", time.Time{}, fmt.Errorf("recovery backup cannot be created in %s: %w", recoveryDir, err)
	}
	if err := ensureAvailableStorage(os.TempDir(), requiredSpace); err != nil {
		return "", time.Time{}, fmt.Errorf("recovery backup cannot use the system temporary folder: %w", err)
	}

	temporaryBackup, err := db.CreateBackupFile()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create fresh-start recovery database: %w", err)
	}
	defer func() {
		_ = os.Remove(temporaryBackup)
	}()

	filename := freshStartRecoveryPrefix + now.Format("2006-01-02-150405") + ".db"
	targetPath := NextAvailableFilePath(recoveryDir, filename)
	if err := CopyFile(temporaryBackup, targetPath); err != nil {
		_ = os.Remove(targetPath)
		return "", time.Time{}, fmt.Errorf("save fresh-start recovery database: %w", err)
	}

	return targetPath, now.Add(FreshStartRecoveryLifetime), nil
}

func resolveFreshStartRecoveryDir() (string, error) {
	rootDir, err := ResolveBackupRootDir()
	if err != nil {
		return "", fmt.Errorf("resolve backup root directory: %w", err)
	}
	recoveryDir := filepath.Join(rootDir, "Ramseyer Finance Backups", freshStartRecoveryFolder)
	if err := os.MkdirAll(recoveryDir, 0o755); err != nil {
		return "", fmt.Errorf("create fresh-start recovery directory: %w", err)
	}
	return recoveryDir, nil
}

// PruneExpiredFreshStartRecoveries deletes only app-created recovery snapshots whose
// modification time has reached the three-day limit. It ignores unrelated files, nested
// folders, and normal Auto/Safety backups so this short retention policy cannot affect the
// user's durable backup history.
func PruneExpiredFreshStartRecoveries(now time.Time) ([]string, error) {
	recoveryDir, err := resolveFreshStartRecoveryDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		return nil, fmt.Errorf("read fresh-start recovery directory: %w", err)
	}

	expiresBefore := now.Add(-FreshStartRecoveryLifetime)
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), freshStartRecoveryPrefix) || !supportedBackupExtension(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(expiresBefore) {
			continue
		}
		path := filepath.Join(recoveryDir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("remove expired fresh-start recovery %s: %w", path, err)
		}
		removed = append(removed, path)
	}
	return removed, nil
}

// StartFreshStartRecoveryCleanupScheduler enforces the three-day expiry while the app is
// running and performs an immediate pass at launch for files that expired while it was
// closed. The one-hour interval keeps deletion close to the promised time without causing
// meaningful filesystem churn.
func StartFreshStartRecoveryCleanupScheduler() func() {
	stop := make(chan struct{})
	go func() {
		cleanup := func() {
			removed, err := PruneExpiredFreshStartRecoveries(time.Now())
			if err != nil {
				log.Printf("fresh-start recovery cleanup failed: %v", err)
				return
			}
			for _, path := range removed {
				log.Printf("expired fresh-start recovery removed: %s", path)
			}
		}

		cleanup()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanup()
			case <-stop:
				return
			}
		}
	}()

	return func() {
		close(stop)
	}
}
