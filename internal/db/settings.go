package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// GetSetting retrieves a single configuration value from the settings table by its key.
// The settings table acts as a lightweight, persistent key-value store for application
// configuration that survives database restores and migrations. Unlike traditional config
// files, storing settings in the database keeps them co-located with the data they govern.
// If the requested key does not exist in the table, the function returns an empty string
// with no error—callers should treat a missing key as "not yet configured" and fall back
// to their own sensible defaults.
func GetSetting(key string) (string, error) {
	// I treat a missing setting as an empty value instead of an error because most settings in
	// this app are optional flags with sensible defaults on first run.
	var value string
	err := DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	// Distinguish between "key not found" (which is a normal, expected state on a fresh
	// install) and actual database failures (which need to be surfaced immediately).
	// sql.ErrNoRows is returned by Scan when the query produces zero rows, and we convert
	// that into a successful empty-string response so callers can use a simple
	// if val == "" { useDefault() } pattern without error-handling boilerplate.
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, nil
}

// SetSetting persists a key-value pair to the settings table. The function uses SQLite's
// UPSERT syntax (INSERT ... ON CONFLICT DO UPDATE) so that callers do not need to check
// whether a key already exists before writing. If the key is new, a row is inserted; if
// the key already exists, its value is updated in place. This atomic upsert also avoids a
// classic read-then-write race condition where two goroutines might both check for existence,
// both see nothing, and then one INSERT succeeds while the other fails with a constraint
// violation.
func SetSetting(key, value string) error {
	// I upsert settings so callers do not need separate create and update flows. The table is
	// intentionally used like a small durable key-value store.
	_, err := DB.Exec(`
		INSERT INTO settings (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}
