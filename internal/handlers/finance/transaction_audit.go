package finance

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"ramseyer-finance/internal/db"
	transactionsaudit "ramseyer-finance/internal/transactions/audit"
	"time"
)

// TransactionAuditEntry represents a single row in the transaction audit log, formatted
// for display in the register page's recent activity sidebar. It carries the audit record's
// own ID and timestamp, the target transaction's ID, the action performed ("created",
// "updated", or "deleted"), the actor identifier ("local operator" for a manual edit or
// "workbook import" for a batch), and the key display fields extracted from the stored JSON
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

// localOperatorActor names the person or process that made a register change. This app has
// no user accounts, so manual edits are attributed to the person at the machine. The
// importer passes a different actor so its rows are distinguishable in the audit trail.
const localOperatorActor = "local operator"

// loadTransactionSnapshot reads the current state of one transaction through the shared
// audit package. The register and the workbook importer must store the same shape, so the
// snapshot struct lives in one place instead of being duplicated here.
func loadTransactionSnapshot(transactionID int64) (transactionsaudit.Snapshot, error) {
	// I snapshot the stored row shape instead of a looser DTO because audit history should reflect
	// exactly what the transaction looked like at that moment, including note mapping metadata.
	return transactionsaudit.Load(db.DB, transactionID)
}

// recordTransactionAudit appends the snapshot to the audit log. The action must be one of
// the shared constants so a typo cannot reach the table's CHECK constraint, and the full
// JSON payload keeps history readable after the original transaction is changed or removed.
func recordTransactionAudit(action string, snapshot transactionsaudit.Snapshot) error {
	// I write the full snapshot at the time of the action instead of trying to reconstruct history later.
	// That keeps the register audit trail defensible even after categories or note references change.
	return transactionsaudit.Record(db.DB, localOperatorActor, action, snapshot)
}

// loadRecentTransactionAuditEntries retrieves the most recent audit log entries (up to the
// specified limit) and formats them for display in the register page's activity sidebar.
// For each entry, it deserialises the stored JSON snapshot to extract the category, type,
// description, and amount for display, and reformats the created_at timestamp into a
// human-readable form. Because the display fields come from the immutable snapshot rather
// than a JOIN against the current transactions table, the audit sidebar correctly shows
// what a transaction looked like at the time of the action, even if the transaction has
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
			snapshot    transactionsaudit.Snapshot
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

// lookupExistingTransaction retrieves a transaction snapshot and distinguishes between a
// genuine database error and the case where the transaction simply does not exist. It is
// used by the update and delete flows, both of which need to load the current row state
// before mutating or removing it. By centralising the "load or not-found" logic, callers
// can use errors.Is(err, sql.ErrNoRows) to decide whether to show a "Transaction not
// found" message or a server error. The function returns sql.ErrNoRows unwrapped so that
// callers can check it directly with errors.Is.
func lookupExistingTransaction(transactionID int64) (transactionsaudit.Snapshot, error) {
	// I wrap the snapshot lookup because update and delete flows both need the same "load or
	// distinguish not-found" behavior before they mutate the row.
	snapshot, err := loadTransactionSnapshot(transactionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return transactionsaudit.Snapshot{}, err
		}
		return transactionsaudit.Snapshot{}, fmt.Errorf("lookup existing transaction: %w", err)
	}
	return snapshot, nil
}
