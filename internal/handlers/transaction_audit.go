package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"ramseyer-finance/internal/db"
	"time"
)

// TransactionAuditEntry represents a single row in the transaction audit log, formatted
// for display in the register page's recent activity sidebar. It carries the audit record's
// own ID and timestamp, the target transaction's ID, the action performed ("created",
// "updated", or "deleted"), the actor identifier (always "local operator" in this
// single-user application), and the key display fields extracted from the stored JSON
// snapshot: category name, transaction type, description, and amount. By decoding these
// fields from the snapshot rather than joining against the live transactions table, the
// audit trail remains accurate even if the original transaction has been modified further
// or deleted since the audit entry was recorded.
type TransactionAuditEntry struct {
	ID            int64
	TransactionID int64
	Action        string
	Actor         string
	CreatedAt     string
	Category      string
	Type          string
	Description   string
	Amount        float64
}

// transactionSnapshot captures the complete state of a single transaction row at a specific
// point in time. It is serialised to JSON and stored in the transaction_audit_log table's
// snapshot_json column, and later deserialised for display in the audit sidebar. The struct
// includes every meaningful column from the transactions table—including category metadata
// and timestamps—so that the audit trail is a self-contained record that does not depend on
// the current state of the transactions or categories tables. The JSON tags use snake_case
// for readability in the stored payload.
type transactionSnapshot struct {
	ID          int64   `json:"id"`
	Date        string  `json:"date"`
	Type        string  `json:"type"`
	Category    string  `json:"category"`
	CategoryID  int64   `json:"category_id"`
	NoteRef     string  `json:"note_ref"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	UpdatedAt   string  `json:"updated_at"`
}

// loadTransactionSnapshot retrieves the current state of a single transaction from the
// database and returns it as a transactionSnapshot struct. All nullable columns are
// COALESCE'd to sensible zero values (empty strings for text, 0 for the category ID) so
// the snapshot is always a complete, serialisable record. The updated_at field falls back
// to created_at when no update has occurred, ensuring the timestamp is never empty. This
// function is called before updates (to record the new state) and before deletes (to
// record what was removed), and it serves as the single point of truth for what
// constitutes a transaction's auditable state.
func loadTransactionSnapshot(transactionID int64) (transactionSnapshot, error) {
	// I snapshot the stored row shape instead of a looser DTO because audit history should reflect
	// exactly what the transaction looked like at that moment, including note mapping metadata.
	var snapshot transactionSnapshot
	err := db.DB.QueryRow(`
		SELECT id, date, type, category, COALESCE(category_id, 0), COALESCE(note_ref, ''), COALESCE(description, ''), amount, COALESCE(updated_at, created_at, '')
		FROM transactions
		WHERE id = ?
	`, transactionID).Scan(
		&snapshot.ID,
		&snapshot.Date,
		&snapshot.Type,
		&snapshot.Category,
		&snapshot.CategoryID,
		&snapshot.NoteRef,
		&snapshot.Description,
		&snapshot.Amount,
		&snapshot.UpdatedAt,
	)
	if err != nil {
		return transactionSnapshot{}, err
	}
	return snapshot, nil
}

// recordTransactionAudit serialises the given transaction snapshot to JSON and inserts a
// new row into the transaction_audit_log table with the action type, actor identifier, and
// the full JSON payload. The snapshot captures the transaction's state at the moment the
// action occurred—after an update, the snapshot reflects the new values; before a delete,
// the snapshot reflects the final values that were removed. Storing the full JSON snapshot
// rather than individual columns means the audit table's schema does not need to change
// when the transactions table evolves, and the audit trail preserves a complete, immutable
// record that can be inspected even if the original row no longer exists.
func recordTransactionAudit(action string, snapshot transactionSnapshot) error {
	// I write the full snapshot at the time of the action instead of trying to reconstruct history later.
	// That keeps the register audit trail defensible even after categories or note references change.
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal transaction audit snapshot: %w", err)
	}

	// The actor is hard-coded as "local operator" because this is a single-user desktop
	// application with no multi-user authentication. If user accounts are added in the
	// future, this would be replaced with the authenticated user's identifier.
	if _, err := db.DB.Exec(`
		INSERT INTO transaction_audit_log (transaction_id, action, actor, snapshot_json)
		VALUES (?, ?, 'local operator', ?)
	`, snapshot.ID, action, string(payload)); err != nil {
		return fmt.Errorf("insert transaction audit log: %w", err)
	}

	return nil
}

// loadRecentTransactionAuditEntries retrieves the most recent audit log entries (up to the
// specified limit) and formats them for display in the register page's activity sidebar.
// For each entry, it deserialises the stored JSON snapshot to extract the category, type,
// description, and amount for display, and reformats the created_at timestamp into a
// human-readable form. Because the display fields come from the immutable snapshot rather
// than a JOIN against the current transactions table, the audit sidebar correctly shows
// what a transaction looked like at the time of the action—even if the transaction has
// since been updated again or deleted entirely.
func loadRecentTransactionAuditEntries(limit int) ([]TransactionAuditEntry, error) {
	// I decode the stored snapshot back into display fields here so the register can show useful
	// recent activity without rejoining against mutable current transaction data.
	rows, err := db.DB.Query(`
		SELECT id, transaction_id, action, actor, snapshot_json, created_at
		FROM transaction_audit_log
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query transaction audit log: %w", err)
	}
	defer rows.Close()

	var entries []TransactionAuditEntry
	for rows.Next() {
		var (
			entry       TransactionAuditEntry
			snapshotRaw string
			snapshot    transactionSnapshot
			createdAt   string
		)
		if err := rows.Scan(&entry.ID, &entry.TransactionID, &entry.Action, &entry.Actor, &snapshotRaw, &createdAt); err != nil {
			return nil, fmt.Errorf("scan transaction audit log: %w", err)
		}
		// Deserialise the JSON snapshot to extract display fields. If the JSON is
		// malformed (which should not happen in normal operation but could occur if
		// the database is manually edited), we return an error rather than silently
		// displaying incomplete data.
		if err := json.Unmarshal([]byte(snapshotRaw), &snapshot); err != nil {
			return nil, fmt.Errorf("decode transaction audit snapshot: %w", err)
		}
		entry.CreatedAt = createdAt
		entry.Category = snapshot.Category
		entry.Type = snapshot.Type
		entry.Description = snapshot.Description
		entry.Amount = snapshot.Amount
		// Reformat the database timestamp from "2006-01-02 15:04:05" to the more
		// readable "02 Jan 2006 15:04" format used throughout the UI. If parsing
		// fails, the original string is left in place.
		if parsed, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
			entry.CreatedAt = parsed.Format("02 Jan 2006 15:04")
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transaction audit log: %w", err)
	}

	return entries, nil
}

// updateTransactionTimestamp sets the updated_at column of a transaction to the current
// local time. This is called after every successful transaction update so that views and
// reports that sort by modification time reflect the most recent change. The timestamp is
// kept separate from the audit log—the audit log records who changed what and when with
// a full snapshot, while updated_at provides a simple, indexed column for cheap sorting
// and filtering without needing to inspect the audit table.
func updateTransactionTimestamp(transactionID int64) error {
	// I keep `updated_at` separate from the audit log so ordinary sorted views can use one cheap
	// column without having to inspect the audit table.
	if _, err := db.DB.Exec(`UPDATE transactions SET updated_at = datetime('now','localtime') WHERE id = ?`, transactionID); err != nil {
		return fmt.Errorf("update transaction timestamp: %w", err)
	}
	return nil
}

// lookupExistingTransaction retrieves a transaction snapshot and distinguishes between a
// genuine database error and the case where the transaction simply does not exist. It is
// used by the update and delete flows, both of which need to load the current row state
// before mutating or removing it. By centralising the "load or not-found" logic, callers
// can use errors.Is(err, sql.ErrNoRows) to decide whether to show a "Transaction not
// found" message or a server error. The function returns sql.ErrNoRows unwrapped so that
// callers can check it directly with errors.Is.
func lookupExistingTransaction(transactionID int64) (transactionSnapshot, error) {
	// I wrap the snapshot lookup because update and delete flows both need the same "load or
	// distinguish not-found" behavior before they mutate the row.
	snapshot, err := loadTransactionSnapshot(transactionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return transactionSnapshot{}, err
		}
		return transactionSnapshot{}, fmt.Errorf("lookup existing transaction: %w", err)
	}
	return snapshot, nil
}
