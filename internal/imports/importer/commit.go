package importer

import (
	"errors"
	"fmt"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/imports/plan"
	transactionsaudit "ramseyer-finance/internal/transactions/audit"
)

const (
	// importActor identifies rows written by a confirmed workbook import in the audit trail.
	// It is deliberately different from the manual register's "local operator" so history
	// shows which changes came from a file.
	importActor = "workbook import"
	// defaultPaymentMethod is the register's neutral classification for an imported
	// collection. The workbook's cash, cheque, and mobile-money analysis is not imported,
	// so claiming a specific method here would invent information.
	defaultPaymentMethod = "cash"
)

// ErrNothingToImport prevents an empty plan from replacing a good batch with no rows.
var ErrNothingToImport = errors.New("this workbook has no dated collection rows to import")

// CommitResult summarises a saved import for the redirect message and tests.
type CommitResult struct {
	BatchID      int64
	Year         int
	RowCount     int
	Total        float64
	ReplacedRows int
}

// Commit validates the label choices and replaces the year's previous import in one
// transaction. The order matters: deleted rows are audited before the old batch is removed,
// the new batch is inserted before its transactions, and the saved label map is written in
// the same transaction so a rollback cannot leave the two out of step.
func Commit(store *PreviewStore, token string, built *plan.Plan, submitted map[string]string) (*CommitResult, error) {
	if built == nil || len(built.Rows) == 0 {
		return nil, ErrNothingToImport
	}

	categories, err := ActiveIncomeCategories(db.DB)
	if err != nil {
		return nil, err
	}
	mapping, mappingErr := resolveMapping(built, categories, submitted)
	if mappingErr != nil {
		return nil, mappingErr
	}

	saved, err := LoadLabelMap()
	if err != nil {
		return nil, err
	}
	_, byID := categoryIndexes(categories)

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin import transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	replacedRows := 0
	existing, err := findBatchForYear(tx, built.Year)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		snapshots, err := transactionsaudit.LoadByImportBatch(tx, existing.ID)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM transactions WHERE import_batch_id = ?`, existing.ID); err != nil {
			return nil, fmt.Errorf("delete transactions for batch %d: %w", existing.ID, err)
		}
		for _, snapshot := range snapshots {
			if err := transactionsaudit.Record(tx, importActor, transactionsaudit.ActionDeleted, snapshot); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM import_batches WHERE id = ?`, existing.ID); err != nil {
			return nil, fmt.Errorf("delete import batch %d: %w", existing.ID, err)
		}
		replacedRows = len(snapshots)
	}

	result, err := tx.Exec(`
		INSERT INTO import_batches (file_name, file_hash, year, row_count, total_amount)
		VALUES (?, ?, ?, ?, ?)
	`, built.FileName, built.FileHash, built.Year, len(built.Rows), built.Total())
	if err != nil {
		return nil, fmt.Errorf("insert import batch: %w", err)
	}
	batchID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read import batch id: %w", err)
	}

	for _, row := range built.Rows {
		category, exists := byID[mapping[row.Key]]
		if !exists {
			return nil, fmt.Errorf("row %q on %s has no resolved account", row.Label, row.Sheet)
		}

		insert, err := tx.Exec(`
			INSERT INTO transactions (
				date, type, category, category_id, payment_method, description, amount,
				note_ref, import_batch_id, updated_at
			)
			VALUES (?, 'income', ?, ?, ?, ?, ?, ?, ?, datetime('now','localtime'))
		`,
			row.Date.Format(plan.DateLayout),
			category.Name,
			category.ID,
			defaultPaymentMethod,
			row.Description(),
			row.Amount,
			category.NoteRef,
			batchID,
		)
		if err != nil {
			return nil, fmt.Errorf("insert %s row %d: %w", row.Sheet, row.Line, err)
		}
		transactionID, err := insert.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("read transaction id for %s row %d: %w", row.Sheet, row.Line, err)
		}
		snapshot, err := transactionsaudit.Load(tx, transactionID)
		if err != nil {
			return nil, fmt.Errorf("load snapshot for imported transaction %d: %w", transactionID, err)
		}
		if err := transactionsaudit.Record(tx, importActor, transactionsaudit.ActionCreated, snapshot); err != nil {
			return nil, err
		}
	}

	if err := saveLabelMap(tx, mergeLabelMap(saved, mapping)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit import: %w", err)
	}

	store.Delete(token)

	return &CommitResult{
		BatchID:      batchID,
		Year:         built.Year,
		RowCount:     len(built.Rows),
		Total:        built.Total(),
		ReplacedRows: replacedRows,
	}, nil
}
