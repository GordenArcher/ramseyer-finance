package backup

import (
	"errors"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"strings"
	"testing"
	"time"
)

func TestCreateFreshStartRecoveryStopsBeforeBackupWhenStorageIsFull(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("RAMSEYER_FINANCE_BACKUP_DIR", backupRoot)
	initializeFreshStartTestDatabase(t)

	originalCheck := ensureAvailableStorage
	ensureAvailableStorage = func(string, uint64) error {
		return errors.New("disk is full")
	}
	t.Cleanup(func() {
		ensureAvailableStorage = originalCheck
	})

	_, _, err := CreateFreshStartRecovery(time.Now())
	if err == nil || !strings.Contains(err.Error(), "disk is full") {
		t.Fatalf("CreateFreshStartRecovery error = %v, want disk-full error", err)
	}

	recoveryDir := filepath.Join(backupRoot, "Ramseyer Finance Backups", freshStartRecoveryFolder)
	entries, readErr := os.ReadDir(recoveryDir)
	if readErr != nil {
		t.Fatalf("read recovery directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("disk-full check created %d recovery files, want 0", len(entries))
	}
}

func TestCreateAndPruneFreshStartRecovery(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("RAMSEYER_FINANCE_BACKUP_DIR", backupRoot)
	initializeFreshStartTestDatabase(t)

	originalCheck := ensureAvailableStorage
	ensureAvailableStorage = func(string, uint64) error { return nil }
	t.Cleanup(func() {
		ensureAvailableStorage = originalCheck
	})

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.Local)
	recoveryPath, expiresAt, err := CreateFreshStartRecovery(now)
	if err != nil {
		t.Fatalf("create fresh-start recovery: %v", err)
	}
	if expiresAt.Sub(now) != FreshStartRecoveryLifetime {
		t.Fatalf("recovery lifetime = %v, want %v", expiresAt.Sub(now), FreshStartRecoveryLifetime)
	}
	if _, err := os.Stat(recoveryPath); err != nil {
		t.Fatalf("stat recovery backup: %v", err)
	}
	candidates, err := LoadRestoreCandidates()
	if err != nil {
		t.Fatalf("load restore candidates: %v", err)
	}
	canonicalRecoveryPath, err := filepath.EvalSymlinks(recoveryPath)
	if err != nil {
		t.Fatalf("resolve recovery path: %v", err)
	}
	foundRecovery := false
	for _, candidate := range candidates {
		if candidate.Path == canonicalRecoveryPath && candidate.Kind == "fresh-start" {
			foundRecovery = true
			break
		}
	}
	if !foundRecovery {
		t.Fatalf("fresh-start recovery %s was not added to the restore library", recoveryPath)
	}
	recoveryDir := filepath.Dir(recoveryPath)
	recentRecovery := filepath.Join(recoveryDir, freshStartRecoveryPrefix+"recent.db")
	unrelatedBackup := filepath.Join(recoveryDir, "ramseyer-finance-manual.db")
	for _, path := range []string{recentRecovery, unrelatedBackup} {
		if err := os.WriteFile(path, []byte("preserve"), 0o600); err != nil {
			t.Fatalf("create retained backup %s: %v", path, err)
		}
	}

	oldTime := now.Add(-FreshStartRecoveryLifetime - time.Minute)
	if err := os.Chtimes(recoveryPath, oldTime, oldTime); err != nil {
		t.Fatalf("age recovery backup: %v", err)
	}
	removed, err := PruneExpiredFreshStartRecoveries(now)
	if err != nil {
		t.Fatalf("prune expired recovery: %v", err)
	}
	if len(removed) != 1 || removed[0] != recoveryPath {
		t.Fatalf("removed = %v, want [%s]", removed, recoveryPath)
	}
	if _, err := os.Stat(recoveryPath); !os.IsNotExist(err) {
		t.Fatalf("expired recovery still exists: %v", err)
	}
	for _, path := range []string{recentRecovery, unrelatedBackup} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cleanup removed retained backup %s: %v", path, err)
		}
	}
}

func initializeFreshStartTestDatabase(t *testing.T) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "fresh-start.db")
	if err := db.Initialize(databasePath); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := db.DB.Exec(`
		INSERT INTO transactions (date, type, category, amount)
		VALUES ('2026-01-01', 'income', 'Test income', 100)
	`); err != nil {
		t.Fatalf("insert test transaction: %v", err)
	}
}
