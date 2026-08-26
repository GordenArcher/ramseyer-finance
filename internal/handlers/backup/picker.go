package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"sort"
	"strings"
	"time"
)

const maxRestoreCandidates = 300

// RestoreCandidate is one backup that the in-app picker can restore. The picker receives
// display-ready metadata as well as the raw modification timestamp used by client-side date
// filters and sorting. The absolute path remains the form value because the restore handler
// revalidates it against the managed backup roots before opening it.
type RestoreCandidate struct {
	ID            string
	Name          string
	Path          string
	Kind          string
	KindLabel     string
	Extension     string
	SizeLabel     string
	ModifiedLabel string
	ModifiedUnix  int64
}

// loadRestoreCandidates builds the custom picker's library from the locations the app
// controls: direct manual downloads, automatic backups, safety snapshots, and successful
// paths retained in backup history. I deduplicate by canonical path because the same file
// normally appears both on disk and in the event log; showing it twice would make users
// wonder whether two distinct restore points exist when they do not.
func LoadRestoreCandidates() ([]RestoreCandidate, error) {
	rootDir, err := ResolveBackupRootDir()
	if err != nil {
		return nil, fmt.Errorf("resolve backup library root: %w", err)
	}

	autoDir := filepath.Join(rootDir, "Ramseyer Finance Backups", "Auto")
	safetyDir := filepath.Join(rootDir, "Ramseyer Finance Backups", "Safety")
	candidatesByPath := map[string]RestoreCandidate{}

	// Manual downloads are written directly into the user's Downloads directory, which may
	// contain unrelated databases. I therefore include only files using this app's filename
	// prefix at the root, while the dedicated Auto and Safety folders can accept every
	// supported SQLite extension.
	if err := collectRestoreDirectory(rootDir, "manual", true, candidatesByPath); err != nil {
		return nil, err
	}
	if err := collectRestoreDirectory(autoDir, "automatic", false, candidatesByPath); err != nil {
		return nil, err
	}
	if err := collectRestoreDirectory(safetyDir, "safety", false, candidatesByPath); err != nil {
		return nil, err
	}

	history, err := LoadBackupHistory(200)
	if err != nil {
		return nil, fmt.Errorf("load backup history for picker: %w", err)
	}
	for _, event := range history {
		if event.Status != "success" || !event.FileFound || strings.TrimSpace(event.FilePath) == "" {
			continue
		}
		kind := restoreKindFromEvent(event.Kind)
		if err := addRestoreCandidate(event.FilePath, kind, candidatesByPath); err != nil {
			// History is durable even after users move files or disconnect external drives.
			// A single stale or unreadable historical path should not make the whole backup
			// screen unavailable, so filesystem failures are intentionally skipped here.
			continue
		}
	}

	candidates := make([]RestoreCandidate, 0, len(candidatesByPath))
	for _, candidate := range candidatesByPath {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ModifiedUnix == candidates[j].ModifiedUnix {
			return strings.ToLower(candidates[i].Name) < strings.ToLower(candidates[j].Name)
		}
		return candidates[i].ModifiedUnix > candidates[j].ModifiedUnix
	})
	if len(candidates) > maxRestoreCandidates {
		candidates = candidates[:maxRestoreCandidates]
	}
	for index := range candidates {
		candidates[index].ID = fmt.Sprintf("restore-candidate-%d", index+1)
	}
	return candidates, nil
}

func collectRestoreDirectory(dir, kind string, requireAppPrefix bool, candidates map[string]RestoreCandidate) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s backup directory: %w", kind, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if requireAppPrefix && !strings.HasPrefix(strings.ToLower(entry.Name()), "ramseyer-finance-") {
			continue
		}
		if !supportedBackupExtension(entry.Name()) {
			continue
		}
		if err := addRestoreCandidate(filepath.Join(dir, entry.Name()), kind, candidates); err != nil {
			return err
		}
	}
	return nil
}

func addRestoreCandidate(path, kind string, candidates map[string]RestoreCandidate) error {
	canonicalPath, err := canonicalExistingFile(path)
	if err != nil {
		return err
	}
	if !supportedBackupExtension(canonicalPath) {
		return nil
	}
	if _, exists := candidates[canonicalPath]; exists {
		return nil
	}
	info, err := os.Stat(canonicalPath)
	if err != nil {
		return err
	}
	candidates[canonicalPath] = RestoreCandidate{
		Name:          filepath.Base(canonicalPath),
		Path:          canonicalPath,
		Kind:          kind,
		KindLabel:     restoreKindLabel(kind),
		Extension:     strings.TrimPrefix(strings.ToLower(filepath.Ext(canonicalPath)), "."),
		SizeLabel:     formatBackupSize(info.Size()),
		ModifiedLabel: info.ModTime().Local().Format("02 Jan 2006, 15:04"),
		ModifiedUnix:  info.ModTime().Unix(),
	}
	return nil
}

func canonicalExistingFile(path string) (string, error) {
	absolutePath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("backup candidate is not a regular file")
	}
	return filepath.Clean(resolvedPath), nil
}

func supportedBackupExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	default:
		return false
	}
}

func restoreKindFromEvent(eventKind string) string {
	switch {
	case strings.HasPrefix(eventKind, "auto-"):
		return "automatic"
	case eventKind == "pre-restore-safety":
		return "safety"
	case eventKind == "manual-download":
		return "manual"
	default:
		return "history"
	}
}

func restoreKindLabel(kind string) string {
	switch kind {
	case "automatic":
		return "Automatic"
	case "safety":
		return "Safety"
	case "manual":
		return "Manual"
	default:
		return "History"
	}
}

func formatBackupSize(size int64) string {
	const (
		kilobyte = 1024
		megabyte = 1024 * kilobyte
	)
	switch {
	case size >= megabyte:
		return fmt.Sprintf("%.1f MB", float64(size)/megabyte)
	case size >= kilobyte:
		return fmt.Sprintf("%.1f KB", float64(size)/kilobyte)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// validateManagedRestorePath is the server-side trust boundary for picker selections.
// A hidden form field can be modified manually, so the UI list alone is not authorization
// to read an arbitrary local file. Managed backup folders and app-prefixed manual downloads
// are accepted directly; a path outside those locations must already exist as a successful
// backup-history record before restore can open it.
func ValidateManagedRestorePath(path string) (string, error) {
	if !supportedBackupExtension(path) {
		return "", fmt.Errorf("Selected file is not a supported SQLite backup")
	}
	canonicalPath, err := canonicalExistingFile(path)
	if err != nil {
		return "", fmt.Errorf("Selected backup file could not be opened")
	}
	rootDir, err := ResolveBackupRootDir()
	if err != nil {
		return "", fmt.Errorf("Backup library could not be resolved")
	}
	canonicalRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("Backup library could not be resolved")
	}
	canonicalRoot, err = filepath.EvalSymlinks(canonicalRoot)
	if err != nil {
		return "", fmt.Errorf("Backup library could not be resolved")
	}
	managedDir := filepath.Join(canonicalRoot, "Ramseyer Finance Backups")
	manualAtRoot := filepath.Dir(canonicalPath) == filepath.Clean(canonicalRoot) &&
		strings.HasPrefix(strings.ToLower(filepath.Base(canonicalPath)), "ramseyer-finance-")
	if pathWithinDirectory(canonicalPath, managedDir) || manualAtRoot {
		return canonicalPath, nil
	}

	var historyCount int
	if err := dbQueryBackupHistoryPath(path, canonicalPath, &historyCount); err != nil {
		return "", fmt.Errorf("Validate selected backup: %w", err)
	}
	if historyCount == 0 {
		return "", fmt.Errorf("Selected file is outside the managed backup library")
	}
	return canonicalPath, nil
}

func dbQueryBackupHistoryPath(originalPath, canonicalPath string, count *int) error {
	return db.DB.QueryRow(`
		SELECT COUNT(*)
		FROM backup_events
		WHERE status = 'success' AND file_path IN (?, ?)
	`, strings.TrimSpace(originalPath), canonicalPath).Scan(count)
}

func pathWithinDirectory(path, directory string) bool {
	relativePath, err := filepath.Rel(filepath.Clean(directory), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relativePath != ".." && relativePath != "." && !strings.HasPrefix(relativePath, ".."+string(os.PathSeparator))
}

// backupAgeDays is kept server-side for tests and future non-JavaScript consumers. The
// browser currently filters with ModifiedUnix so the visible results update instantly.
func backupAgeDays(now time.Time, candidate RestoreCandidate) int {
	if candidate.ModifiedUnix <= 0 {
		return 0
	}
	age := now.Sub(time.Unix(candidate.ModifiedUnix, 0))
	if age < 0 {
		return 0
	}
	return int(age.Hours() / 24)
}
