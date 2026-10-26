// Package audit owns the immutable snapshot shape stored in transaction_audit_log. Both
// the manual register and the workbook importer write history in this format, so the
// fields live here instead of being copied into each feature.
package audit

import (
	"database/sql"
	"fmt"
)

// Snapshot is the complete state of a transaction at one point in time. The JSON tags are
// part of the stored contract: the register reads history back through these names even
// after the transactions table changes.
type Snapshot struct {
	ID            int64   `json:"id"`
	Date          string  `json:"date"`
	Type          string  `json:"type"`
	Category      string  `json:"category"`
	CategoryID    int64   `json:"category_id"`
	PaymentMethod string  `json:"payment_method"`
	NoteRef       string  `json:"note_ref"`
	Description   string  `json:"description"`
	Amount        float64 `json:"amount"`
	UpdatedAt     string  `json:"updated_at"`
}

// Queryer is the part of *sql.DB and *sql.Tx that Load needs. Keeping it narrow lets the
// importer load a snapshot inside its own transaction instead of opening a second view of
// the database.
type Queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

// RowsQueryer is the part of *sql.DB and *sql.Tx that LoadByImportBatch needs. It stays
// separate from Queryer because a batch read returns many rows.
type RowsQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// Load reads one transaction into the shared snapshot shape. Nullable columns are
// coalesced so the stored JSON is always complete, and updated_at falls back to created_at
// for rows written before that column existed.
func Load(queryer Queryer, transactionID int64) (Snapshot, error) {
	var snapshot Snapshot
	err := queryer.QueryRow(`
		SELECT id, date, type, category, COALESCE(category_id, 0),
			COALESCE(NULLIF(payment_method, ''), 'cash'),
			COALESCE(note_ref, ''), COALESCE(description, ''), amount,
			COALESCE(updated_at, created_at, '')
		FROM transactions
		WHERE id = ?
	`, transactionID).Scan(
		&snapshot.ID,
		&snapshot.Date,
		&snapshot.Type,
		&snapshot.Category,
		&snapshot.CategoryID,
		&snapshot.PaymentMethod,
		&snapshot.NoteRef,
		&snapshot.Description,
		&snapshot.Amount,
		&snapshot.UpdatedAt,
	)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// LoadByImportBatch reads every transaction belonging to an import batch. The importer
// uses it before deleting a replaced batch so the audit trail still explains what was
// removed, even though the rows themselves are gone.
func LoadByImportBatch(queryer RowsQueryer, batchID int64) ([]Snapshot, error) {
	rows, err := queryer.Query(`
		SELECT id, date, type, category, COALESCE(category_id, 0),
			COALESCE(NULLIF(payment_method, ''), 'cash'),
			COALESCE(note_ref, ''), COALESCE(description, ''), amount,
			COALESCE(updated_at, created_at, '')
		FROM transactions
		WHERE import_batch_id = ?
		ORDER BY id
	`, batchID)
	if err != nil {
		return nil, fmt.Errorf("query transactions for import batch %d: %w", batchID, err)
	}
	defer rows.Close()

	snapshots := make([]Snapshot, 0)
	for rows.Next() {
		var snapshot Snapshot
		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.Date,
			&snapshot.Type,
			&snapshot.Category,
			&snapshot.CategoryID,
			&snapshot.PaymentMethod,
			&snapshot.NoteRef,
			&snapshot.Description,
			&snapshot.Amount,
			&snapshot.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transaction for import batch %d: %w", batchID, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transactions for import batch %d: %w", batchID, err)
	}
	return snapshots, nil
}
