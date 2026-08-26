package backup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Settings keys for the automatic backup feature. These are stored in the database settings
// table so that backup preferences survive application restarts, database restores, and
// migrations. The keys track whether auto-backup is enabled, how frequently it should run,
// and when the last backup was successfully completed.
const (
	settingAutoBackupEnabled   = "auto_backup_enabled"
	settingAutoBackupFrequency = "auto_backup_frequency"
	SettingAutoBackupLastRun   = "auto_backup_last_run"
)

// AutoBackupConfig holds all the display and scheduling parameters for the automatic backup
// feature. It is constructed by loadAutoBackupConfig from the database settings and from
// the filesystem (directory resolution). The struct is designed to be passed directly to
// the settings UI template, so it includes pre-formatted display labels and human-readable
// interval text alongside the raw scheduling values.
type AutoBackupConfig struct {
	Enabled           bool
	Frequency         string
	LastRunAt         time.Time
	LastRunLabel      string
	Directory         string
	CheckIntervalText string
}

// loadAutoBackupConfig reads the three backup settings from the database, parses and
// normalises them, resolves the backup directory on the filesystem, and returns a fully
// populated AutoBackupConfig. Missing settings are treated as defaults: auto-backup is
// disabled, frequency defaults to weekly, and last-run shows "Never". Directory resolution
// failures are non-fatal—the field is simply left empty so the UI can show a warning.
func LoadAutoBackupConfig() (AutoBackupConfig, error) {
	// I store backup preferences in SQLite because they are product behavior, not machine setup.
	// Keeping them alongside the rest of the app data means a restored database also restores
	// the backup policy the user was actually relying on.
	enabledValue, err := db.GetSetting(settingAutoBackupEnabled)
	if err != nil {
		return AutoBackupConfig{}, err
	}
	frequencyValue, err := db.GetSetting(settingAutoBackupFrequency)
	if err != nil {
		return AutoBackupConfig{}, err
	}
	lastRunValue, err := db.GetSetting(SettingAutoBackupLastRun)
	if err != nil {
		return AutoBackupConfig{}, err
	}

	// Parse the enabled flag with a generous boolean parser that accepts "1", "true",
	// "yes", and "on" (case-insensitive). Anything else is treated as disabled, making
	// the default safe: if the setting is missing or garbled, backups won't run.
	config := AutoBackupConfig{
		Enabled:   parseSettingBool(enabledValue),
		Frequency: NormalizeAutoBackupFrequency(frequencyValue),
	}
	if config.Frequency == "" {
		config.Frequency = "weekly"
	}

	// Parse the last-run timestamp from RFC 3339 format. If parsing fails (e.g., the
	// setting was corrupted or manually edited), we silently fall back to "Never" rather
	// than aborting the whole config load—the scheduler will just treat the next run as
	// immediately due and correct the stored value.
	if strings.TrimSpace(lastRunValue) != "" {
		lastRunAt, err := time.Parse(time.RFC3339, lastRunValue)
		if err == nil {
			config.LastRunAt = lastRunAt
			config.LastRunLabel = lastRunAt.Local().Format("02 Jan 2006 15:04")
		}
	}
	if config.LastRunLabel == "" {
		config.LastRunLabel = "Never"
	}

	// Resolve the auto-backup directory. If this fails (e.g., no home directory on a
	// headless system), we leave Directory empty so the UI can surface the problem
	// without crashing the settings page.
	directory, err := resolveAutoBackupDir()
	if err == nil {
		config.Directory = directory
	}
	config.CheckIntervalText = strings.Title(config.Frequency)

	return config, nil
}

// normalizeAutoBackupFrequency maps a raw frequency string to one of the three recognised
// values: "weekly", "monthly", or "quarterly". Any unrecognised input (including empty
// strings) defaults to "weekly". This function is the single point of normalisation so
// that every other function can switch on a known, limited set of values.
func NormalizeAutoBackupFrequency(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "weekly":
		return "weekly"
	case "monthly":
		return "monthly"
	case "quarterly":
		return "quarterly"
	default:
		return "weekly"
	}
}

// parseSettingBool converts a stored setting string into a boolean. It accepts "1", "true",
// "yes", and "on" (case-insensitive) as truthy values, and treats everything else—including
// empty strings, "0", "false", and "no"—as false. This generous parsing makes the settings
// table resilient to manual edits and future format changes.
func parseSettingBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// autoBackupInterval returns the minimum duration that must elapse between automatic
// backups for a given frequency. Weekly is 7 days, monthly is 30 days, and quarterly is
// 90 days. These are approximate calendar intervals—no attempt is made to align with
// calendar month boundaries, as the scheduling is based purely on elapsed time since the
// last successful backup.
func autoBackupInterval(frequency string) time.Duration {
	switch NormalizeAutoBackupFrequency(frequency) {
	case "monthly":
		return 30 * 24 * time.Hour
	case "quarterly":
		return 90 * 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// autoBackupRetention returns the maximum number of backup files to keep for each
// frequency bucket. Weekly and default both keep 12 backups (roughly 3 months of weekly
// snapshots), monthly keeps 12 (one year), and quarterly keeps 8 (two years). These
// limits are enforced independently per frequency, so switching from weekly to monthly
// does not cause the old weekly files to be pruned by the monthly retention rule.
func autoBackupRetention(frequency string) int {
	switch NormalizeAutoBackupFrequency(frequency) {
	case "monthly":
		return 12
	case "quarterly":
		return 8
	default:
		return 12
	}
}

// saveAutoBackupConfig persists the enabled flag and frequency to the database settings
// table. It normalises the frequency before writing, ensuring that only recognised values
// are stored. The last-run timestamp is not updated here—that is managed separately by
// RunAutoBackupIfDue after a successful backup completes.
func SaveAutoBackupConfig(enabled bool, frequency string) error {
	if err := db.SetSetting(settingAutoBackupEnabled, strconv.FormatBool(enabled)); err != nil {
		return err
	}
	if err := db.SetSetting(settingAutoBackupFrequency, NormalizeAutoBackupFrequency(frequency)); err != nil {
		return err
	}
	return nil
}

// StartAutoBackupScheduler launches a background goroutine that periodically checks
// whether an automatic backup is due. It returns a stop function that callers can invoke
// to gracefully shut down the scheduler (e.g., during application shutdown). The scheduler
// performs an immediate check on startup to catch any backups that should have run while
// the application was closed, then checks every 12 hours thereafter. All errors are logged
// but do not crash the scheduler—a failed backup attempt does not prevent future attempts.
func StartAutoBackupScheduler() func() {
	stop := make(chan struct{})

	go func() {
		// I run a due check immediately on startup because a ticker only works while the app is open.
		// This startup pass is what closes the gap when the user was away for longer than the schedule.
		if ran, path, err := RunAutoBackupIfDue(time.Now()); err != nil {
			log.Printf("auto backup startup check failed: %v", err)
		} else if ran {
			log.Printf("auto backup created at %s", path)
		}

		// I keep the periodic check coarse because the real scheduling rule lives in RunAutoBackupIfDue.
		// Waking up too often would add churn without creating better backups.
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ran, path, err := RunAutoBackupIfDue(time.Now())
				if err != nil {
					log.Printf("auto backup periodic check failed: %v", err)
					continue
				}
				if ran {
					log.Printf("auto backup created at %s", path)
				}
			case <-stop:
				return
			}
		}
	}()

	return func() {
		close(stop)
	}
}

// RunAutoBackupIfDue checks whether an automatic backup should be performed at the given
// time. It loads the current configuration, verifies that auto-backup is enabled, compares
// the elapsed time since the last successful backup against the configured interval, and—
// if the interval has elapsed—creates a new backup. The function records the outcome in
// both the database (updating the last-run timestamp) and the backup event log. It is
// designed to be called from both the startup check and the periodic ticker without
// duplicating backups.
func RunAutoBackupIfDue(now time.Time) (bool, string, error) {
	config, err := LoadAutoBackupConfig()
	if err != nil {
		return false, "", fmt.Errorf("load auto backup config: %w", err)
	}
	if !config.Enabled {
		return false, "", nil
	}
	// I compare against the persisted last-run time instead of process uptime so the decision
	// survives restarts and does not create duplicates within the same backup window.
	if !config.LastRunAt.IsZero() && now.Sub(config.LastRunAt) < autoBackupInterval(config.Frequency) {
		return false, "", nil
	}

	path, err := createAutoBackup(now, config.Frequency)
	if err != nil {
		_ = RecordBackupEvent("auto-"+config.Frequency, "failed", "", err.Error())
		return false, "", err
	}
	// Persist the completion timestamp immediately so that a crash after this point does
	// not cause the scheduler to create a duplicate backup on the next startup check.
	if err := db.SetSetting(SettingAutoBackupLastRun, now.UTC().Format(time.RFC3339)); err != nil {
		return false, "", fmt.Errorf("save auto backup last run: %w", err)
	}
	_ = RecordBackupEvent("auto-"+config.Frequency, "success", path, "Automatic backup created")

	return true, path, nil
}

// createAutoBackup performs the actual work of generating an automatic backup file. It
// delegates to db.CreateBackupFile for a consistent, WAL-safe SQLite snapshot, copies the
// resulting temp file into the auto-backup directory with a timestamped filename, and then
// runs retention pruning to remove old backups that exceed the configured limit. The
// function cleans up the SQLite temp file regardless of success or failure.
func createAutoBackup(now time.Time, frequency string) (string, error) {
	// I always create backups through SQLite's own backup path so WAL mode is handled safely.
	// Copying the live database file directly would risk partial snapshots.
	backupPath, err := db.CreateBackupFile()
	if err != nil {
		return "", fmt.Errorf("create sqlite backup: %w", err)
	}
	defer func() {
		_ = os.Remove(backupPath)
	}()

	// I keep automatic backups in their own directory so retention can prune them without
	// interfering with manual downloads or pre-restore safety copies.
	autoBackupDir, err := resolveAutoBackupDir()
	if err != nil {
		return "", fmt.Errorf("resolve auto backup directory: %w", err)
	}

	// The filename encodes the frequency and a sortable timestamp (down to the second) so
	// that alphabetical order matches chronological order, making retention pruning simple
	// and the directory listing human-readable.
	filename := fmt.Sprintf(
		"ramseyer-finance-auto-%s-%s.db",
		NormalizeAutoBackupFrequency(frequency),
		now.Format("2006-01-02-150405"),
	)
	targetPath := filepath.Join(autoBackupDir, filename)
	if err := CopyFile(backupPath, targetPath); err != nil {
		return "", fmt.Errorf("save auto backup: %w", err)
	}

	// Retention cleanup runs after the new backup is written, not before, so a full
	// directory does not prevent the current backup from being saved. If pruning fails, we
	// log the error but do not fail the backup—a stale file is better than no backup.
	if err := pruneAutoBackups(autoBackupDir, NormalizeAutoBackupFrequency(frequency), autoBackupRetention(frequency)); err != nil {
		log.Printf("auto backup retention cleanup failed: %v", err)
	}

	return targetPath, nil
}

// resolveAutoBackupDir locates (or creates) the directory where automatic backups are
// stored. It delegates to resolveBackupRootDir for the platform-specific base path, then
// appends "Ramseyer Finance Backups/Auto" to namespace the auto-backups separately from
// any manual backups the user might create. If the directory doesn't exist, it is created
// with permissions 0755 (owner read/write/execute, group and others read/execute).
func resolveAutoBackupDir() (string, error) {
	rootDir, err := ResolveBackupRootDir()
	if err != nil {
		return "", err
	}

	autoBackupDir := filepath.Join(rootDir, "Ramseyer Finance Backups", "Auto")
	if err := os.MkdirAll(autoBackupDir, 0o755); err != nil {
		return "", fmt.Errorf("create auto backup directory: %w", err)
	}
	return autoBackupDir, nil
}

// pruneAutoBackups removes the oldest automatic backup files for a given frequency until
// only the specified number remain. It filters the target directory to only consider files
// matching the frequency-specific prefix (e.g., "ramseyer-finance-auto-weekly-"), sorts
// them by modification time with the newest first, and deletes any beyond the retention
// limit. This per-frequency filtering ensures that switching backup schedules does not
// cause cross-frequency file deletion.
func pruneAutoBackups(dir, frequency string, keep int) error {
	if keep <= 0 {
		return nil
	}

	// I prune within each frequency bucket independently so quarterly archives are not deleted
	// just because weekly backups are more numerous.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read auto backup directory: %w", err)
	}

	prefix := "ramseyer-finance-auto-" + NormalizeAutoBackupFrequency(frequency) + "-"
	type backupEntry struct {
		path    string
		modTime time.Time
	}

	// Collect all backup files that match this frequency's naming pattern. We use
	// modification time rather than parsing the filename timestamp because it's simpler
	// and equally accurate—the file is written once and never modified afterward.
	var backups []backupEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupEntry{
			path:    filepath.Join(dir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	// If we're at or under the retention limit, there's nothing to delete. This also
	// handles the common case on a fresh install where there are zero backup files.
	if len(backups) <= keep {
		return nil
	}

	// Sort descending by modification time so the newest backups are at the front of the
	// slice. We keep the first `keep` entries and delete everything after that.
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})

	// Delete the oldest backups, skipping any that have already been removed (e.g., by a
	// concurrent process or a previous partial cleanup). os.IsNotExist is treated as a
	// success, since the goal—removing the file—has already been achieved.
	for _, oldBackup := range backups[keep:] {
		if err := os.Remove(oldBackup.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove old auto backup %s: %w", oldBackup.path, err)
		}
	}

	return nil
}
